"""QA release contract. No secret values are read by the deployment identity."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time

ROOT = Path(__file__).resolve().parents[1]
TARGETS = {('nss-sso', 'identity-api'), ('nss-sso', 'platform-api'),
           ('nss-sso', 'event-worker'), ('sso-bff', 'sso-bff')}
DB_KEYS = ('DB_HOST', 'DB_PORT', 'DB_NAME', 'DB_USER', 'DB_PASSWORD', 'DB_TLS_SERVER_NAME')
PUBLIC_KEYS = {'AUTH_ORIGIN', 'CONSOLE_ORIGIN', 'DB_TLS_CA_FILE', 'HYDRA_ADMIN_URL',
               'HYDRA_PUBLIC_URL', 'OIDC_ISSUER', 'SSO_ENABLED', 'SSO_MODE',
               'MAIL_ACCOUNT_URL', 'SMTP_HOST', 'SMTP_PORT', 'PLATFORM_API_URL', 'OAUTH_CLIENT_ID'}


def kubectl(args, body=None, missing=False):
    context = ['--context', os.environ['QA_KUBE_CONTEXT']] if os.environ.get('QA_KUBE_CONTEXT') else []
    result = subprocess.run(['kubectl', *context, *args], input=json.dumps(body) if body is not None else None,
                            text=True, capture_output=True)
    if result.returncode:
        if missing and '(NotFound)' in result.stderr:
            return None
        # Do not echo Kubernetes errors, objects or commands that might contain configuration.
        raise RuntimeError('Kubernetes operation failed: ' + args[0])
    return json.loads(result.stdout) if result.stdout.strip() else None


def get(kind, name, ns, missing=False):
    return kubectl(['get', kind, name, '-n', ns, '-o', 'json'], missing=missing)


def load():
    maps = json.loads((ROOT / 'deploy/qa/configmaps.json').read_text())['items']
    runtime = json.loads((ROOT / 'deploy/qa/runtime.json').read_text())
    if len(runtime) != 4 or {(r['namespace'], r['deployment']) for r in runtime} != TARGETS:
        raise ValueError('Unexpected deployment scope')
    by_name = {(c['metadata']['namespace'], c['metadata']['name']): c for c in maps}
    if len(maps) != 4 or len(by_name) != 4:
        raise ValueError('Expected four owned ConfigMaps')
    for r in runtime:
        ns, name = r['namespace'], r['deployment']
        expected_cm = name + '-config'
        if r['container'] != name or r['configMap'] != expected_cm or r['secret'] != name + '-runtime':
            raise ValueError('Unexpected runtime binding')
        cm = by_name[(ns, expected_cm)]
        if cm['kind'] != 'ConfigMap' or cm['apiVersion'] != 'v1' or cm.get('binaryData'):
            raise ValueError('Invalid ConfigMap contract')
        if not cm['data'] or not set(cm['data']).issubset(PUBLIC_KEYS):
            raise ValueError('ConfigMap contains an unreviewed or sensitive key')
        for value in cm['data'].values():
            if not isinstance(value, str) or re.search(r'-----BEGIN|://[^/\s]+:[^/@\s]+@|[?&](?:token|password|secret)=', value, re.I):
                raise ValueError('Potential sensitive value in ConfigMap')
        if set(cm['data']) & set(r['secretKeys']) or not set(DB_KEYS).issubset(r['secretKeys']):
            raise ValueError('Secret/configuration key conflict')
    if set(by_name) != {(r['namespace'], r['configMap']) for r in runtime}:
        raise ValueError('ConfigMap scope mismatch')
    return runtime, by_name


def image(service):
    ref = (Path(os.environ['RELEASE_DIR']) / service).read_text().strip()
    repo = os.environ['GITHUB_REPOSITORY'].lower()
    if not re.fullmatch(re.escape('ghcr.io/' + repo + '-' + service + '@sha256:') + '[a-f0-9]{64}', ref):
        raise ValueError('Invalid release image: ' + service)
    return ref


def environment(r, cm):
    values = [{'name': key, 'valueFrom': {'configMapKeyRef': {'name': r['configMap'], 'key': key}}}
              for key in sorted(cm['data'])]
    values += [{'name': key, 'valueFrom': {'secretKeyRef': {'name': r['secret'], 'key': key}}}
               for key in sorted(r['secretKeys'])]
    return values


def checksum(r, cm):
    return hashlib.sha256(json.dumps([cm['data'], environment(r, cm)], sort_keys=True).encode()).hexdigest()


def migration_job(ns, group, ref, dep):
    pod = dep['spec']['template']['spec']
    ca = 'sso-cloudsql-ca' if ns == 'nss-sso' else 'sso-bff-cloudsql-ca'
    return {'apiVersion': 'batch/v1', 'kind': 'Job', 'metadata': {
        'name': 'sso-schema-migrate', 'namespace': ns,
        'labels': {'app.kubernetes.io/managed-by': 'sso-ci', 'sso-release': os.environ['RELEASE_SHA']}},
        'spec': {'backoffLimit': 0, 'activeDeadlineSeconds': 660, 'ttlSecondsAfterFinished': 86400,
                 'template': {'metadata': {'labels': {'app': 'sso-schema-migrate'}}, 'spec': {
                     'restartPolicy': 'Never', 'automountServiceAccountToken': False,
                     'nodeSelector': {'kubernetes.io/os': 'linux', 'kubernetes.io/arch': 'amd64'},
                     'hostAliases': pod.get('hostAliases', []), 'imagePullSecrets': pod.get('imagePullSecrets', []),
                     'securityContext': {'runAsUser': 65532, 'runAsGroup': 65532, 'runAsNonRoot': True,
                                         'seccompProfile': {'type': 'RuntimeDefault'}},
                     'containers': [{'name': 'migrate', 'image': ref, 'imagePullPolicy': 'IfNotPresent',
                         'env': [{'name': 'SSO_MODE', 'value': 'kubernetes'},
                                 {'name': 'MIGRATION_GROUP', 'value': group},
                                 {'name': 'DB_TLS_CA_FILE', 'value': '/etc/cloudsql-ca/cloudsql-ca.pem'}] + [
                                     {'name': key, 'valueFrom': {'secretKeyRef': {'name': 'identity-api-runtime' if ns == 'nss-sso' else 'sso-bff-runtime', 'key': key}}}
                                     for key in DB_KEYS],
                         'resources': {'requests': {'cpu': '50m', 'memory': '64Mi'},
                                       'limits': {'cpu': '500m', 'memory': '256Mi'}},
                         'securityContext': {'allowPrivilegeEscalation': False, 'readOnlyRootFilesystem': True,
                                             'capabilities': {'drop': ['ALL']}},
                         'volumeMounts': [{'name': 'cloudsql-ca', 'mountPath': '/etc/cloudsql-ca', 'readOnly': True}]}],
                     'volumes': [{'name': 'cloudsql-ca', 'secret': {'secretName': ca}}]}}}}


def reconcile():
    runtime, maps = load()
    migration_image = image('db-migrate')
    live_deps, live_maps, refs = {}, {}, {}
    # Validate all existing targets and artifacts before starting DDL or touching live configuration.
    for r in runtime:
        ns, name = r['namespace'], r['deployment']
        live_deps[(ns, name)] = get('deployment', name, ns)
        if not any(c['name'] == name for c in live_deps[(ns, name)]['spec']['template']['spec']['containers']):
            raise ValueError('Missing managed container')
        live_maps[(ns, r['configMap'])] = get('configmap', r['configMap'], ns)
        if live_maps[(ns, r['configMap'])].get('immutable'):
            raise ValueError('Managed ConfigMap is immutable')
        refs[(ns, name)] = image('console-bff' if name == 'sso-bff' else name)
    jobs = []
    for ns, group, source in [('nss-sso', 'business', 'identity-api'), ('sso-bff', 'bff', 'sso-bff')]:
        existing = get('job', 'sso-schema-migrate', ns, missing=True)
        if existing:
            terminal = any(c['type'] in ('Complete', 'Failed') and c['status'] == 'True'
                           for c in existing.get('status', {}).get('conditions', []))
            if not terminal:
                raise RuntimeError('Previous migration is still active in ' + ns)
        jobs.append((ns, group, existing, migration_job(ns, group, migration_image, live_deps[(ns, source)])))
    for ns, group, existing, job in jobs:
        if existing:
            kubectl(['delete', 'job', 'sso-schema-migrate', '-n', ns, '--wait=true', '-o', 'json'])
        kubectl(['create', '-f', '-', '-o', 'json'], job)
        print('Started migration gate: ' + ns + '/' + group, flush=True)
    deadline = time.monotonic() + 720
    pending = {ns for ns, _, _, _ in jobs}
    while pending:
        for ns in list(pending):
            job = get('job', 'sso-schema-migrate', ns)
            if job['metadata']['labels'].get('sso-release') != os.environ['RELEASE_SHA']:
                raise RuntimeError('Migration release mismatch')
            for condition in job.get('status', {}).get('conditions', []):
                if condition['status'] != 'True':
                    continue
                if condition['type'] == 'Failed':
                    raise RuntimeError('Migration failed in ' + ns + '; configuration and images were not changed')
                if condition['type'] == 'Complete':
                    pending.remove(ns)
                    print('Migration verified: ' + ns, flush=True)
        if time.monotonic() >= deadline:
            raise RuntimeError('Migration gate timed out; configuration and images were not changed')
        if pending:
            time.sleep(5)
    configure(runtime, maps, live_maps, refs)


def configure(runtime, maps, live_maps, refs):
    for r in runtime:
        ns, name = r['namespace'], r['deployment']
        key = (ns, r['configMap'])
        cm = json.loads(json.dumps(maps[key]))
        cm['metadata']['resourceVersion'] = live_maps[key]['metadata']['resourceVersion']
        kubectl(['replace', '-f', '-', '-o', 'json'], cm)
        actual = get('configmap', r['configMap'], ns)
        if actual.get('data') != cm['data'] or actual.get('binaryData'):
            raise RuntimeError('ConfigMap readback mismatch')
    for r in runtime:
        ns, name = r['namespace'], r['deployment']
        cm = maps[(ns, r['configMap'])]
        patch = {'spec': {'template': {'metadata': {'annotations': {'sso-ci/config-sha256': checksum(r, cm)}},
            'spec': {'containers': [{'name': name, 'image': refs[(ns, name)],
                                    'envFrom': None, 'env': [{'$patch': 'replace'}, *environment(r, cm)]}]}}}}
        kubectl(['patch', 'deployment', name, '-n', ns, '--type=strategic', '-p', json.dumps(patch), '-o', 'json'])
        actual = get('deployment', name, ns)
        container = next(c for c in actual['spec']['template']['spec']['containers'] if c['name'] == name)
        if container['env'] != environment(r, cm) or container.get('envFrom'):
            raise RuntimeError('Runtime references readback mismatch')
        print('Reconciled configuration and image: ' + ns + '/' + name, flush=True)
