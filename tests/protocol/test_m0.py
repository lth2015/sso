"""Real-engine M0 tests. All identities/clients are synthetic and local-only.

Never log protocol bodies: they contain secrets. No mock OAuth server is used.
"""
import base64
import hashlib
import http.cookiejar
import http.client
import json
from pathlib import Path
import secrets
import subprocess
import time
import unittest
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
PUBLIC = 'http://127.0.0.1:4444'
ADMIN = 'http://127.0.0.1:4445'
AUD = 'https://api.m0.invalid'
CALLBACK = 'http://127.0.0.1:3000/callback'
ENV = dict(line.split('=', 1) for line in (ROOT / '.local/m0.env').read_text().splitlines())
COMPOSE = ['docker', 'compose', '--env-file', str(ROOT / '.local/m0.env'), '-f', str(ROOT / 'deploy/local/compose.yml')]


def compose(*args, **kw):
    return subprocess.run(COMPOSE + list(args), check=True, capture_output=True, **kw)


def sql(statement):
    # SQL never includes client secrets. Password is expanded only inside the container.
    result = compose('exec', '-T', 'mysql', 'sh', '-c',
                     'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -N -B sso_m0',
                     input=statement.encode())
    return result.stdout.decode().strip()


def request(url, method='GET', body=None, headers=None, opener=None):
    data = None
    h = dict(headers or {})
    if body is not None:
        if isinstance(body, dict):
            data = json.dumps(body).encode()
            h['Content-Type'] = 'application/json'
        else:
            data = body.encode()
    req = urllib.request.Request(url, data=data, headers=h, method=method)
    try:
        r = (opener or urllib.request.build_opener()).open(req, timeout=25)
    except urllib.error.HTTPError as e:
        r = e
    with r:
        return r.status, r.headers, r.read()


def api(path, body=None, method=None):
    status, _, data = request(ADMIN + path, method or ('POST' if body is not None else 'GET'), body)
    if status not in (200, 201, 204):
        raise AssertionError(f'Admin operation failed: HTTP {status}')
    return json.loads(data) if data else None


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class ProtocolTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        status, _, body = request(PUBLIC + '/.well-known/openid-configuration')
        assert status == 200
        cls.discovery = json.loads(body)
        assert cls.discovery['issuer'] == PUBLIC + '/'

    def setUp(self):
        self.clients = []
        self.run_id = secrets.token_hex(8)

    def tearDown(self):
        # Restore fault-injection targets even when assertions fail.
        compose('start', 'mysql', 'hook')
        for client in self.clients:
            request(ADMIN + '/admin/clients/' + client, method='DELETE')
            sql(f"DELETE FROM policies WHERE client_id='{client}';")

    def client(self, kind='service', tenant='tenant-a', policy=True):
        client = 'm0-' + self.run_id + '-' + str(len(self.clients))
        secret = secrets.token_urlsafe(32)
        self.clients.append(client)
        scopes = 'read' if kind == 'service' else 'openid offline_access read'
        api('/admin/clients', {
            'client_id': client, 'client_secret': secret,
            'grant_types': ['client_credentials'] if kind == 'service' else ['authorization_code', 'refresh_token'],
            'response_types': [] if kind == 'service' else ['code'],
            'redirect_uris': [] if kind == 'service' else [CALLBACK],
            'scope': scopes, 'audience': [AUD],
            'token_endpoint_auth_method': 'client_secret_basic',
        })
        if policy:
            sql(f"INSERT INTO policies VALUES('{client}','{tenant}','principal-a','{kind}','user-a','ctx-{client}','{AUD}','{scopes}',TRUE,1,{int(time.time())+3600});")
        return client, secret

    def token(self, client, secret, **fields):
        auth = base64.b64encode(f'{client}:{secret}'.encode()).decode()
        status, _, data = request(PUBLIC + '/oauth2/token', 'POST',
            urllib.parse.urlencode(fields), {'Authorization': 'Basic ' + auth, 'Content-Type': 'application/x-www-form-urlencoded'})
        result = json.loads(data)
        return status, result

    def m2m(self, client, secret, **kw):
        return self.token(client, secret, grant_type='client_credentials', scope='read', audience=AUD, **kw)

    def code(self, client, *, tenant_hint=None, pkce=True, context=None, bad_redirect=False):
        verifier = secrets.token_urlsafe(40)
        state, nonce = secrets.token_urlsafe(20), secrets.token_urlsafe(20)
        query = dict(client_id=client, response_type='code', redirect_uri=CALLBACK + ('/evil' if bad_redirect else ''),
                     scope='openid offline_access read', audience=AUD, state=state, nonce=nonce)
        if pkce:
            query.update(code_challenge=base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b'=').decode(), code_challenge_method='S256')
        if tenant_hint:
            query['tenant_id'] = tenant_hint
        browser = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()), NoRedirect())
        url = PUBLIC + '/oauth2/auth?' + urllib.parse.urlencode(query)
        for _ in range(15):
            parsed = urllib.parse.urlparse(url)
            args = urllib.parse.parse_qs(parsed.query)
            if url.startswith(CALLBACK):
                if 'code' not in args:
                    return None, verifier, nonce
                self.assertEqual(args.get('state'), [state])
                return args['code'][0], verifier, nonce
            if parsed.port == 3000:
                if 'login_challenge' in args:
                    challenge = args['login_challenge'][0]
                    info = api('/admin/oauth2/auth/requests/login?login_challenge=' + challenge)
                    self.assertEqual(info['client']['client_id'], client)
                    url = api('/admin/oauth2/auth/requests/login/accept?login_challenge=' + challenge,
                              {'subject': 'user-a', 'remember': False}, 'PUT')['redirect_to']
                elif 'consent_challenge' in args:
                    challenge = args['consent_challenge'][0]
                    info = api('/admin/oauth2/auth/requests/consent?consent_challenge=' + challenge)
                    self.assertEqual(info['subject'], 'user-a')
                    url = api('/admin/oauth2/auth/requests/consent/accept?consent_challenge=' + challenge,
                        {'grant_scope': info['requested_scope'], 'grant_access_token_audience': info['requested_access_token_audience'],
                         'remember': False, 'session': {'access_token': {'m0_context_id': context or 'ctx-' + client,
                             'm0_grant_audience': AUD, 'm0_grant_scopes': info['requested_scope']}}}, 'PUT')['redirect_to']
                else:
                    return None, verifier, nonce
            else:
                status, headers, _ = request(url, opener=browser)
                if status not in (302, 303):
                    return None, verifier, nonce
                url = urllib.parse.urljoin(url, headers['Location'])
        raise AssertionError('Authorization did not complete within redirect limit')

    def redeem(self, client, secret, code, verifier):
        return self.token(client, secret, grant_type='authorization_code', code=code, redirect_uri=CALLBACK, code_verifier=verifier)

    def denied(self, result):
        status, body = result
        self.assertGreaterEqual(status, 400)
        self.assertNotIn('access_token', body)
        self.assertNotIn('refresh_token', body)
        self.assertIn('error', body)

    def claims(self, token, expected_tenant='tenant-a', expected_subject=None, expected_nonce=None):
        # Verify RS256 cryptographically with the JWKS fetched from the fixed issuer.
        parts = token.split('.')
        self.assertEqual(len(parts), 3)
        def decode(s):
            return base64.urlsafe_b64decode(s + '=' * (-len(s) % 4))
        header, payload = (json.loads(decode(p)) for p in parts[:2])
        self.assertEqual(header['alg'], 'RS256')
        status, _, raw = request(PUBLIC + '/.well-known/jwks.json')
        self.assertEqual(status, 200)
        jwk = next(k for k in json.loads(raw)['keys'] if k['kid'] == header['kid'])
        # Use OpenSSL already provided by the environment, avoiding an unpinned Python package.
        def der(tag, data):
            length = len(data)
            size = bytes([length]) if length < 128 else (lambda x: bytes([128 + len(x)]) + x)(length.to_bytes((length.bit_length()+7)//8,'big'))
            return bytes([tag]) + size + data
        def integer(value):
            data=decode(value)
            return der(2, (b'\0' if data[0]&128 else b'')+data)
        key=der(48,integer(jwk['n'])+integer(jwk['e']))
        directory=ROOT/'.local/verify';directory.mkdir(exist_ok=True)
        pub=directory/(self.run_id+'.der');sig=directory/(self.run_id+'.sig')
        try:
            pub.write_bytes(key);sig.write_bytes(decode(parts[2]))
            result=subprocess.run(['openssl','dgst','-sha256','-verify',str(pub),'-keyform','DER','-signature',str(sig)],
                input=(parts[0]+'.'+parts[1]).encode(),capture_output=True)
            self.assertEqual(result.returncode,0,'JWT signature verification failed')
        finally:
            pub.unlink(missing_ok=True);sig.unlink(missing_ok=True)
        self.assertEqual(payload['iss'],PUBLIC+'/')
        self.assertGreater(payload['exp'],int(time.time()))
        # Hydra persists expiry and creates iat at different precision boundaries.
        # Observed rounding is at most one second; do not grant a generic long leeway.
        self.assertLessEqual(payload['exp']-payload['iat'],301)
        if expected_nonce is not None:
            self.assertEqual(payload['nonce'],expected_nonce)
        else:
            self.assertEqual(payload['aud'],[AUD])
            self.assertEqual(payload['ext']['tenant_id'],expected_tenant)
        if expected_subject:
            self.assertEqual(payload['sub'],expected_subject)
        return payload

    def test_01_discovery_and_mysql_version(self):
        self.assertIn('S256', self.discovery['code_challenge_methods_supported'])
        self.assertTrue(sql('SELECT VERSION();').startswith('8.4.'))
        self.assertGreater(int(sql('SELECT COUNT(*) FROM hydra.schema_migration;')),0)

    def test_02_m2m_signed_claims_and_tenant_override(self):
        client, secret = self.client()
        status, body = self.m2m(client,secret,tenant_id='tenant-b')
        self.assertEqual(status,200)
        claims=self.claims(body['access_token'],expected_subject=client)
        self.assertEqual(claims['ext']['principal_type'],'service')
        self.assertNotIn('refresh_token',body)
        self.assertGreater(int(sql(f"SELECT COUNT(*) FROM hook_observations WHERE client_id='{client}' AND grant_type='client_credentials' AND allowed=1;")),0)

    def test_03_m2m_disabled_expired_and_orphan(self):
        client, secret=self.client()
        sql(f"UPDATE policies SET active=FALSE WHERE client_id='{client}';")
        self.denied(self.m2m(client,secret))
        sql(f"UPDATE policies SET active=TRUE,expires_at=1 WHERE client_id='{client}';")
        self.denied(self.m2m(client,secret))
        orphan, orphan_secret=self.client(policy=False)
        self.denied(self.m2m(orphan,orphan_secret))

    def test_04_scope_audience_and_password_rejected(self):
        client, secret=self.client()
        for fields in [dict(grant_type='client_credentials',scope='write',audience=AUD),
                       dict(grant_type='client_credentials',scope='read',audience='https://evil.invalid'),
                       dict(grant_type='password',username='user-a',password='not-a-password')]:
            self.denied(self.token(client,secret,**fields))

    def test_05_code_pkce_nonce_and_refresh_claims(self):
        client, secret=self.client('user')
        code,verifier,nonce=self.code(client,tenant_hint='tenant-b')
        self.assertIsNotNone(code,'code flow did not reach callback')
        status,body=self.redeem(client,secret,code,verifier)
        self.assertEqual(status,200,body.get('error'))
        self.claims(body['access_token'],expected_subject='user-a')
        identity=self.claims(body['id_token'],expected_subject='user-a',expected_nonce=nonce)
        self.assertIn(client,identity['aud'])
        refreshed=self.token(client,secret,grant_type='refresh_token',refresh_token=body['refresh_token'],tenant_id='tenant-b')
        self.assertEqual(refreshed[0],200,refreshed[1].get('error'))
        self.claims(refreshed[1]['access_token'],expected_subject='user-a')
        self.assertNotEqual(body['refresh_token'],refreshed[1]['refresh_token'])
        self.assertEqual(sql(f"SELECT COUNT(DISTINCT grant_type) FROM hook_observations WHERE client_id='{client}' AND allowed=1;"),'2')

    def test_06_pkce_missing_wrong_and_redirect_rejected(self):
        client,secret=self.client('user')
        code,_,_=self.code(client,pkce=False)
        self.assertIsNone(code,'Missing PKCE unexpectedly allowed')
        code,verifier,_=self.code(client)
        self.assertIsNotNone(code)
        self.denied(self.redeem(client,secret,code,'x'*50))
        code,_,_=self.code(client,bad_redirect=True)
        self.assertIsNone(code)

    def test_07_code_reuse_rejected(self):
        client,secret=self.client('user')
        code,verifier,_=self.code(client)
        self.assertIsNotNone(code)
        self.assertEqual(self.redeem(client,secret,code,verifier)[0],200)
        self.denied(self.redeem(client,secret,code,verifier))

    def test_08_code_and_refresh_disabled_by_hook(self):
        client,secret=self.client('user')
        code,verifier,_=self.code(client)
        self.assertIsNotNone(code)
        sql(f"UPDATE policies SET active=FALSE WHERE client_id='{client}';")
        self.denied(self.redeem(client,secret,code,verifier))
        sql(f"UPDATE policies SET active=TRUE WHERE client_id='{client}';")
        code,verifier,_=self.code(client)
        status,body=self.redeem(client,secret,code,verifier)
        self.assertEqual(status,200,body.get('error'))
        sql(f"UPDATE policies SET active=FALSE WHERE client_id='{client}';")
        self.denied(self.token(client,secret,grant_type='refresh_token',refresh_token=body['refresh_token']))
        self.assertEqual(sql(f"SELECT COUNT(DISTINCT grant_type) FROM hook_observations WHERE client_id='{client}' AND allowed=0;"),'2')

    def test_09_refresh_reuse_revokes_chain(self):
        client,secret=self.client('user')
        code,verifier,_=self.code(client)
        status,body=self.redeem(client,secret,code,verifier)
        self.assertEqual(status,200)
        status,new=self.token(client,secret,grant_type='refresh_token',refresh_token=body['refresh_token'])
        self.assertEqual(status,200)
        self.denied(self.token(client,secret,grant_type='refresh_token',refresh_token=body['refresh_token']))
        self.denied(self.token(client,secret,grant_type='refresh_token',refresh_token=new['refresh_token']))

    def test_10_wrong_context_rejected(self):
        client,secret=self.client('user')
        code,verifier,_=self.code(client,context='wrong-context')
        self.assertIsNotNone(code)
        self.denied(self.redeem(client,secret,code,verifier))

    def test_11_hook_disconnect_fails_closed_all_grants(self):
        machine,msecret=self.client()
        user,usecret=self.client('user')
        code,verifier,_=self.code(user)
        status,body=self.redeem(user,usecret,code,verifier)
        self.assertEqual(status,200)
        other,osecret=self.client('user')
        code,verifier,_=self.code(other)
        self.assertIsNotNone(code)
        compose('stop','hook')
        try:
            import concurrent.futures
            calls=[lambda:self.m2m(machine,msecret),
                   lambda:self.redeem(other,osecret,code,verifier),
                   lambda:self.token(user,usecret,grant_type='refresh_token',refresh_token=body['refresh_token'])]
            def no_token(call):
                try:
                    self.denied(call())
                except http.client.RemoteDisconnected:
                    # Distinct fault outcome: server closed without an HTTP response.
                    # This is not counted as an OAuth error response or latency success.
                    pass
            with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
                list(pool.map(no_token,calls))
        finally:
            compose('start','hook')
        self.assertEqual(self.m2m(machine,msecret)[0],200)

    def test_12_unauthenticated_hook_rejected(self):
        status,_,_=request('http://127.0.0.1:8081/token-hook','POST',{})
        self.assertEqual(status,401)

    def test_13_concurrent_m2m(self):
        import concurrent.futures
        client,secret=self.client()
        with concurrent.futures.ThreadPoolExecutor(max_workers=5) as pool:
            results=list(pool.map(lambda _:self.m2m(client,secret),range(10)))
        self.assertTrue(all(status==200 and 'access_token' in body for status,body in results))
        self.assertEqual(sql(f"SELECT COUNT(*) FROM hook_observations WHERE client_id='{client}' AND allowed=1;"),'10')

    def test_14_protocol_client_removal(self):
        client,secret=self.client()
        api('/admin/clients/'+client,method='DELETE')
        self.denied(self.m2m(client,secret))

    def test_15_hook_db_connection_recovery(self):
        # Terminate existing app connections; pool must reconnect to the real DB.
        client,secret=self.client()
        self.assertEqual(self.m2m(client,secret)[0],200)
        ids=sql("SELECT ID FROM information_schema.PROCESSLIST WHERE USER='m0_hook';").splitlines()
        for conn in ids:
            if conn.isdigit():
                sql('KILL '+conn+';')
        self.assertEqual(self.m2m(client,secret)[0],200)

    def test_16_authority_timeout_fails_closed_all_grants(self):
        import concurrent.futures
        machine,msecret=self.client()
        user,usecret=self.client('user')
        code,verifier,_=self.code(user)
        status,body=self.redeem(user,usecret,code,verifier)
        self.assertEqual(status,200)
        other,osecret=self.client('user')
        code,verifier,_=self.code(other)
        self.assertIsNotNone(code)
        marker='m0-lock-'+self.run_id
        sleep_query='SELECT SLEEP(120) /*'+marker+'*/'
        command=COMPOSE+['exec','-T','mysql','sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot -N -B sso_m0']
        blocker=subprocess.Popen(command,stdin=subprocess.PIPE,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        blocker.stdin.write(("START TRANSACTION; SELECT client_id FROM policies WHERE client_id IN ('"+machine+"','"+user+"','"+other+"') FOR UPDATE; "+sleep_query+"; ROLLBACK;\n").encode())
        blocker.stdin.close()
        connection=None
        try:
            for _ in range(50):
                connection=sql("SELECT ID FROM information_schema.PROCESSLIST WHERE INFO='"+sleep_query+"';")
                if connection.isdigit(): break
                time.sleep(.1)
            self.assertTrue(connection and connection.isdigit(),'Lock holder did not initialize')
            calls=[lambda:self.m2m(machine,msecret),
                   lambda:self.redeem(other,osecret,code,verifier),
                   lambda:self.token(user,usecret,grant_type='refresh_token',refresh_token=body['refresh_token'])]
            with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
                for result in pool.map(lambda call:call(),calls): self.denied(result)
        finally:
            if connection and connection.isdigit(): sql('KILL '+connection+';')
            blocker.wait(timeout=5)
