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
            'post_logout_redirect_uris': [] if kind=='service' else [CALLBACK+'/logout'],
            'scope': scopes, 'audience': [AUD],
            'token_endpoint_auth_method': 'client_secret_basic',
        })
        if policy:
            sql(f"INSERT INTO policies VALUES('{client}','{tenant}','principal-a','{kind}','user-a','ctx-{client}','{AUD}','{scopes}',TRUE,1,{int(time.time())+3600});")
        if policy and kind == 'user':
            sql(f"INSERT INTO tenant_qualifications VALUES('{client}','{tenant}','user-a',TRUE,1);")
        return client, secret

    def token(self, client, secret, **fields):
        auth = base64.b64encode(f'{client}:{secret}'.encode()).decode()
        status, _, data = request(PUBLIC + '/oauth2/token', 'POST',
            urllib.parse.urlencode(fields), {'Authorization': 'Basic ' + auth, 'Content-Type': 'application/x-www-form-urlencoded'})
        result = json.loads(data)
        return status, result

    def m2m(self, client, secret, **kw):
        return self.token(client, secret, grant_type='client_credentials', scope='read', audience=AUD, **kw)

    def code(self, client, *, tenant_hint=None, pkce=True, context=None, bad_redirect=False, browser=None, remember=False, prompt=None, max_age=None, selected_tenant='tenant-a', interactive=True):
        verifier = secrets.token_urlsafe(40)
        state, nonce = secrets.token_urlsafe(20), secrets.token_urlsafe(20)
        query = dict(client_id=client, response_type='code', redirect_uri=CALLBACK + ('/evil' if bad_redirect else ''),
                     scope='openid offline_access read', audience=AUD, state=state, nonce=nonce)
        if pkce:
            query.update(code_challenge=base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b'=').decode(), code_challenge_method='S256')
        if tenant_hint:
            query['tenant_id'] = tenant_hint
        if prompt is not None: query['prompt']=prompt
        if max_age is not None: query['max_age']=str(max_age)
        self.last_auth={'login_skips':[],'consent_skips':[], 'error':None}
        browser = browser or urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()), NoRedirect())
        url = PUBLIC + '/oauth2/auth?' + urllib.parse.urlencode(query)
        for _ in range(15):
            parsed = urllib.parse.urlparse(url)
            args = urllib.parse.parse_qs(parsed.query)
            if (parsed.scheme,parsed.netloc,parsed.path)==('http','127.0.0.1:3000','/callback'):
                if 'code' not in args:
                    self.last_auth['error']=args.get('error',[None])[0]
                    return None, verifier, nonce
                self.assertEqual(args.get('state'), [state])
                return args['code'][0], verifier, nonce
            if parsed.port == 3000:
                if 'login_challenge' in args:
                    challenge = args['login_challenge'][0]
                    info = api('/admin/oauth2/auth/requests/login?login_challenge=' + challenge)
                    self.assertEqual(info['client']['client_id'], client)
                    self.last_auth['login_skips'].append(info['skip'])
                    if not interactive and not info['skip']:
                        url=api('/admin/oauth2/auth/requests/login/reject?login_challenge='+challenge,{'error':'login_required'},'PUT')['redirect_to']
                        continue
                    url = api('/admin/oauth2/auth/requests/login/accept?login_challenge=' + challenge,
                              {'subject': info['subject'] if info['skip'] else 'user-a', 'remember': remember, 'remember_for':3600}, 'PUT')['redirect_to']
                elif 'consent_challenge' in args:
                    challenge = args['consent_challenge'][0]
                    info = api('/admin/oauth2/auth/requests/consent?consent_challenge=' + challenge)
                    self.assertEqual(info['subject'], 'user-a')
                    self.last_auth['consent_skips'].append(info['skip'])
                    if not interactive and not info['skip']:
                        url=api('/admin/oauth2/auth/requests/consent/reject?consent_challenge='+challenge,{'error':'consent_required'},'PUT')['redirect_to']
                        continue
                    # Persist a fresh immutable authorization context for this transaction.
                    # Only this trusted lab adapter creates bindings; hints never choose the tenant.
                    context_id=secrets.token_hex(32)
                    qualification=sql(f"SELECT security_version FROM tenant_qualifications WHERE client_id='{client}' AND tenant_id='{selected_tenant}' AND subject_id='user-a' AND active=TRUE;")
                    if not qualification.isdigit(): raise AssertionError('No active synthetic tenant qualification')
                    version=sql(f"SELECT security_version FROM policies WHERE client_id='{client}';")
                    binding=hashlib.sha256(info['challenge'].encode()).hexdigest()
                    granted_scopes=' '.join(info['requested_scope'])
                    sql(f"INSERT INTO authorization_contexts VALUES('{context_id}','{client}','{selected_tenant}','user-a','principal-a',UNHEX('{binding}'),NULL,'{info['login_session_id']}','{AUD}','{granted_scopes}',TRUE,{qualification},{version},{int(time.time())+3600});")
                    self.last_context=context_id
                    url = api('/admin/oauth2/auth/requests/consent/accept?consent_challenge=' + challenge,
                        {'grant_scope': info['requested_scope'], 'grant_access_token_audience': info['requested_access_token_audience'],
                         'remember': remember, 'remember_for':3600, 'session': {'access_token': {'m0_context_id': context or context_id}}}, 'PUT')['redirect_to']
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

    def test_17_independent_transactions_tenants_and_refresh_chains(self):
        client,secret=self.client('user')
        sql(f"INSERT INTO tenant_qualifications VALUES('{client}','tenant-b','user-a',TRUE,1);")
        code_a,ver_a,_=self.code(client)
        context_a=self.last_context
        code_b,ver_b,_=self.code(client,selected_tenant='tenant-b')
        context_b=self.last_context
        self.assertNotEqual(context_a,context_b)
        a=self.redeem(client,secret,code_a,ver_a)
        b=self.redeem(client,secret,code_b,ver_b)
        self.assertEqual(a[0],200,a[1].get('error'))
        self.assertEqual(b[0],200,b[1].get('error'))
        self.claims(a[1]['access_token'],'tenant-a')
        self.claims(b[1]['access_token'],'tenant-b')
        sql(f"UPDATE authorization_contexts SET active=FALSE WHERE id='{context_a}';")
        self.denied(self.token(client,secret,grant_type='refresh_token',refresh_token=a[1]['refresh_token']))
        refreshed=self.token(client,secret,grant_type='refresh_token',refresh_token=b[1]['refresh_token'],tenant_id='tenant-a')
        self.assertEqual(refreshed[0],200)
        self.claims(refreshed[1]['access_token'],'tenant-b')

    def test_18_bound_context_cannot_be_reused_for_another_code(self):
        client,secret=self.client('user')
        code,verifier,_=self.code(client)
        first=self.last_context
        self.assertEqual(self.redeem(client,secret,code,verifier)[0],200)
        # Even the privileged lab adapter cannot bind another engine chain to this context.
        code,verifier,_=self.code(client,context=first)
        self.denied(self.redeem(client,secret,code,verifier))

    def test_19_qualification_version_and_absolute_expiry(self):
        for change in ['qualification_version','context_expiry','client_version']:
            with self.subTest(change=change):
                client,secret=self.client('user')
                code,verifier,_=self.code(client)
                status,body=self.redeem(client,secret,code,verifier)
                self.assertEqual(status,200)
                if change=='qualification_version':
                    sql(f"UPDATE tenant_qualifications SET security_version=security_version+1 WHERE client_id='{client}';")
                elif change=='context_expiry':
                    sql(f"UPDATE authorization_contexts SET absolute_expires_at=1 WHERE id='{self.last_context}';")
                else:
                    sql(f"UPDATE policies SET security_version=security_version+1 WHERE client_id='{client}';")
                self.denied(self.token(client,secret,grant_type='refresh_token',refresh_token=body['refresh_token']))

    def test_20_stored_context_cannot_exceed_client_boundary(self):
        for column,value in [('audience','https://evil.invalid'),('scopes','read write')]:
            with self.subTest(column=column):
                client,secret=self.client('user')
                code,verifier,_=self.code(client)
                sql(f"UPDATE authorization_contexts SET {column}='{value}' WHERE id='{self.last_context}';")
                self.denied(self.redeem(client,secret,code,verifier))

    def browser(self):
        return urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),NoRedirect())

    def test_21_remembered_login_consent_and_second_application(self):
        browser=self.browser()
        client,secret=self.client('user')
        code,verifier,_=self.code(client,browser=browser,remember=True)
        self.assertEqual(self.last_auth['login_skips'],[False])
        self.assertEqual(self.last_auth['consent_skips'],[False])
        self.assertEqual(self.redeem(client,secret,code,verifier)[0],200)
        code,verifier,_=self.code(client,browser=browser,remember=True,prompt='none',interactive=False)
        self.assertIsNotNone(code)
        self.assertEqual(self.last_auth['login_skips'],[True])
        self.assertEqual(self.last_auth['consent_skips'],[True])
        self.assertEqual(self.redeem(client,secret,code,verifier)[0],200)
        other,osecret=self.client('user')
        code,verifier,_=self.code(other,browser=browser,remember=True)
        self.assertEqual(self.last_auth['login_skips'],[True])
        self.assertEqual(self.last_auth['consent_skips'],[False])
        self.assertEqual(self.redeem(other,osecret,code,verifier)[0],200)

    def test_22_prompt_none_and_max_age(self):
        browser=self.browser()
        client,secret=self.client('user')
        code,_,_=self.code(client,browser=browser,prompt='none',interactive=False)
        self.assertIsNone(code)
        self.assertEqual(self.last_auth['error'],'login_required')
        code,verifier,_=self.code(client,browser=browser,remember=True)
        self.assertEqual(self.redeem(client,secret,code,verifier)[0],200)
        code,_,_=self.code(client,browser=browser,prompt='none',max_age=0,interactive=False)
        self.assertIsNone(code)
        self.assertEqual(self.last_auth['error'],'login_required')

    def logout(self,browser,token,client):
        state=secrets.token_urlsafe(20)
        url=PUBLIC+'/oauth2/sessions/logout?'+urllib.parse.urlencode({'id_token_hint':token,'post_logout_redirect_uri':CALLBACK+'/logout','state':state})
        for _ in range(10):
            p=urllib.parse.urlparse(url);args=urllib.parse.parse_qs(p.query)
            if (p.netloc,p.path)==('127.0.0.1:3000','/callback/logout'):
                self.assertEqual(args.get('state'),[state]);return
            if (p.netloc,p.path)==('127.0.0.1:3000','/logout'):
                challenge=args['logout_challenge'][0]
                info=api('/admin/oauth2/auth/requests/logout?logout_challenge='+challenge)
                self.assertEqual(info['subject'],'user-a')
                self.assertTrue(info.get('sid'),'Logout has no trusted session binding')
                # Platform-side logout revokes all authorization contexts of this
                # central session before accepting the protocol logout. Engine
                # logout alone is not claimed to be refresh-chain revocation.
                sql(f"UPDATE authorization_contexts SET active=FALSE WHERE login_session_id='{info['sid']}' AND subject_id='user-a';")
                url=api('/admin/oauth2/auth/requests/logout/accept?logout_challenge='+challenge,{},'PUT')['redirect_to']
            else:
                status,headers,_=request(url,opener=browser)
                self.assertIn(status,(302,303))
                url=urllib.parse.urljoin(url,headers['Location'])
        self.fail('Logout did not reach the registered callback')

    def test_23_logout_adapter_revokes_context_and_remembered_login(self):
        browser=self.browser()
        client,secret=self.client('user')
        code,verifier,_=self.code(client,browser=browser,remember=True)
        status,body=self.redeem(client,secret,code,verifier)
        self.assertEqual(status,200)
        self.logout(browser,body['id_token'],client)
        self.denied(self.token(client,secret,grant_type='refresh_token',refresh_token=body['refresh_token']))
        # Already-issued JWTs remain cryptographically valid within their TTL.
        self.claims(body['access_token'])
        code,_,_=self.code(client,browser=browser,prompt='none',interactive=False)
        self.assertIsNone(code)
        self.assertEqual(self.last_auth['error'],'login_required')

    def test_24_consent_revocation_rejects_refresh(self):
        client,secret=self.client('user')
        code,verifier,_=self.code(client,remember=True)
        status,body=self.redeem(client,secret,code,verifier)
        self.assertEqual(status,200)
        api('/admin/oauth2/auth/sessions/consent?'+urllib.parse.urlencode({'subject':'user-a','client':client}),method='DELETE')
        self.denied(self.token(client,secret,grant_type='refresh_token',refresh_token=body['refresh_token']))

    def test_25_signing_key_rotation_keeps_previous_public_key(self):
        client,secret=self.client()
        status,body=self.m2m(client,secret)
        self.assertEqual(status,200)
        old=body['access_token']
        header=lambda token:json.loads(base64.urlsafe_b64decode(token.split('.')[0]+'=='))
        old_kid=header(old)['kid']
        new_kid='m0-rotation-'+self.run_id
        # Never print/store Admin's returned private key material.
        api('/admin/keys/hydra.jwt.access-token',{'alg':'RS256','use':'sig','kid':new_kid})
        status,new=self.m2m(client,secret)
        self.assertEqual(status,200)
        self.assertNotEqual(header(new['access_token'])['kid'],old_kid)
        status,_,data=request(PUBLIC+'/.well-known/jwks.json')
        self.assertEqual(status,200)
        keys=json.loads(data)['keys']
        self.assertTrue(any(k['kid']==old_kid for k in keys))
        self.assertTrue(all(not any(field in k for field in ('d','p','q','dp','dq','qi')) for k in keys))
        self.claims(old)
        self.claims(new['access_token'])
        # Keep both keys: pruning before the token trust window ends is unsafe.

    def test_26_unbound_refresh_and_wrong_session_subject_rejected(self):
        client,secret=self.client('user')
        code,_,_=self.code(client)
        self.assertIsNotNone(code)
        context=self.last_context
        sid=sql(f"SELECT login_session_id FROM authorization_contexts WHERE id='{context}';")
        for grant,subject,session in [('refresh_token','user-a',sid),('authorization_code','user-b',sid),('authorization_code','user-a','wrong-session')]:
            payload={'request':{'client_id':client,'grant_types':[grant]},'session':{'id_token':{'subject':subject,'id_token_claims':{'ext':{'sid':session}}},'extra':{'m0_context_id':context},'consent_challenge':'test-cannot-activate-context'}}
            status,_,_=request('http://127.0.0.1:8081/token-hook','POST',payload,{'X-Hook-Key':ENV['HOOK_KEY']})
            self.assertEqual(status,403)
            self.assertEqual(sql(f"SELECT protocol_binding IS NULL FROM authorization_contexts WHERE id='{context}';"),'1')

    def test_27_qualification_suspend_is_tenant_scoped(self):
        client,secret=self.client('user')
        sql(f"INSERT INTO tenant_qualifications VALUES('{client}','tenant-b','user-a',TRUE,1);")
        first,verifier,_=self.code(client)
        a=self.redeem(client,secret,first,verifier)
        second,verifier,_=self.code(client,selected_tenant='tenant-b')
        b=self.redeem(client,secret,second,verifier)
        self.assertEqual(a[0],200)
        self.assertEqual(b[0],200)
        sql(f"UPDATE tenant_qualifications SET active=FALSE WHERE client_id='{client}' AND tenant_id='tenant-a';")
        self.denied(self.token(client,secret,grant_type='refresh_token',refresh_token=a[1]['refresh_token']))
        status,body=self.token(client,secret,grant_type='refresh_token',refresh_token=b[1]['refresh_token'])
        self.assertEqual(status,200)
        self.claims(body['access_token'],'tenant-b')

    def test_28_refresh_scope_cannot_exceed_original_consent(self):
        client,secret=self.client('user')
        code,verifier,_=self.code(client)
        status,body=self.redeem(client,secret,code,verifier)
        self.assertEqual(status,200)
        self.denied(self.token(client,secret,grant_type='refresh_token',refresh_token=body['refresh_token'],scope='read write'))
