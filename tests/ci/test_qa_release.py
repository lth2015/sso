"""Verify release gates and destructive configuration replacement without cluster access."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('qa_release', ROOT / 'scripts/qa_release.py')
qa = importlib.util.module_from_spec(spec)
spec.loader.exec_module(qa)


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        self.runtime, self.maps = qa.load()
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        for service in ('identity-api', 'platform-api', 'event-worker', 'console-bff', 'db-migrate'):
            (Path(self.temp.name) / service).write_text('ghcr.io/lth2015/sso-' + service + '@sha256:' + 'a' * 64)
        self.env = patch.dict(os.environ, RELEASE_DIR=self.temp.name, RELEASE_SHA='a' * 40, GITHUB_REPOSITORY='lth2015/sso')
        self.env.start()
        self.addCleanup(self.env.stop)
        self.actions = []
        self.objects = {}
        self.fail_migration = False
        for r in self.runtime:
            ns, name = r['namespace'], r['deployment']
            self.objects[('deployment', ns, name)] = {'metadata': {}, 'spec': {'template': {'spec': {'containers': [{'name': name}]}}}}
            cm = copy.deepcopy(self.maps[(ns, r['configMap'])])
            cm['metadata']['resourceVersion'] = '1'
            cm['data']['STALE_KEY'] = 'must be removed'
            self.objects[('configmap', ns, r['configMap'])] = cm

    def get(self, kind, name, ns, missing=False):
        self.actions.append(('get', kind, ns, name))
        return copy.deepcopy(self.objects.get((kind, ns, name)))

    def kubectl(self, args, body=None, missing=False):
        action = args[0]
        self.actions.append((action, body))
        if action == 'create':
            ns = body['metadata']['namespace']
            job = copy.deepcopy(body)
            job['status'] = {'conditions': [{'type': 'Failed' if self.fail_migration else 'Complete', 'status': 'True'}]}
            self.objects[('job', ns, 'sso-schema-migrate')] = job
        elif action == 'replace':
            self.objects[('configmap', body['metadata']['namespace'], body['metadata']['name'])] = copy.deepcopy(body)
        elif action == 'patch':
            name, ns = args[2], args[4]
            payload = json.loads(args[args.index('-p') + 1])
            container = payload['spec']['template']['spec']['containers'][0]
            container['env'] = container['env'][1:]  # Kubernetes strategic list replacement directive.
            container.pop('envFrom', None)
            self.objects[('deployment', ns, name)]['spec']['template'] = payload['spec']['template']
        else:
            raise AssertionError('Unexpected mutation')
        return body

    def test_failed_migration_leaves_all_configuration_and_images_untouched(self):
        before = copy.deepcopy(self.objects)
        self.fail_migration = True
        with patch.object(qa, 'get', self.get), patch.object(qa, 'kubectl', self.kubectl):
            with self.assertRaisesRegex(RuntimeError, 'Migration failed'):
                qa.reconcile()
        self.assertFalse(any(a[0] in ('replace', 'patch') for a in self.actions))
        for key, value in before.items():
            self.assertEqual(self.objects[key], value)

    def test_full_map_replacement_and_combined_image_environment_update(self):
        with patch.object(qa, 'get', self.get), patch.object(qa, 'kubectl', self.kubectl):
            qa.reconcile()
        replacements = [i for i, a in enumerate(self.actions) if a[0] == 'replace']
        patches = [i for i, a in enumerate(self.actions) if a[0] == 'patch']
        creates = [i for i, a in enumerate(self.actions) if a[0] == 'create']
        self.assertEqual(len(replacements), 4)
        self.assertEqual(len(patches), 4)
        self.assertLess(max(creates), min(replacements))
        self.assertLess(max(replacements), min(patches))
        for r in self.runtime:
            cm = self.objects[('configmap', r['namespace'], r['configMap'])]
            self.assertEqual(cm['data'], self.maps[(r['namespace'], r['configMap'])]['data'])
            container = self.objects[('deployment', r['namespace'], r['deployment'])]['spec']['template']['spec']['containers'][0]
            self.assertTrue(all('value' not in e for e in container['env']))

    def test_invalid_migration_image_blocks_all_cluster_mutations(self):
        (Path(self.temp.name) / 'db-migrate').write_text('ghcr.io/another/repo@sha256:' + 'a' * 64)
        with patch.object(qa, 'get', self.get), patch.object(qa, 'kubectl', self.kubectl):
            with self.assertRaises(ValueError):
                qa.reconcile()
        self.assertEqual(self.actions, [])

    def test_sensitive_config_key_is_rejected(self):
        # Read the real contract through a temporary root; never modify repository fixtures.
        with tempfile.TemporaryDirectory() as td:
            root = Path(td)
            (root / 'deploy/qa').mkdir(parents=True)
            maps = {'items': list(copy.deepcopy(self.maps).values())}
            maps['items'][0]['data']['DB_PASSWORD'] = 'not-a-real-secret'
            (root / 'deploy/qa/configmaps.json').write_text(json.dumps(maps))
            (root / 'deploy/qa/runtime.json').write_text(json.dumps(self.runtime))
            with patch.object(qa, 'ROOT', root), self.assertRaisesRegex(ValueError, 'sensitive'):
                qa.load()


if __name__ == '__main__':
    unittest.main()
