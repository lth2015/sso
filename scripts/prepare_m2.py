"""Generate a durable local notification encryption key. Never print its value."""
from pathlib import Path
import secrets
import hashlib
import os
import tempfile
root=Path(__file__).resolve().parents[1]
env=root/'.local/m0.env'
values=dict(line.split('=',1) for line in env.read_text().splitlines())
marker=root/'.local/account-key.id'
if 'IDENTITY_MAIL_KEY' not in values:
    if marker.exists():raise SystemExit('Existing account key binding is missing; restore the original key instead of regenerating it')
    values['IDENTITY_MAIL_KEY']=secrets.token_hex(32)
    with env.open('a') as f:f.write('IDENTITY_MAIL_KEY='+values['IDENTITY_MAIL_KEY']+'\n')
key=values['IDENTITY_MAIL_KEY']
if len(key)!=64 or any(c not in '0123456789abcdef' for c in key):raise SystemExit('Invalid local account key encoding')
fingerprint=hashlib.sha256(bytes.fromhex(key)).hexdigest()
if marker.exists():
    if marker.read_text()!=fingerprint:raise SystemExit('Account key changed; restore the original key or implement an explicit migration')
else:
    fd=os.open(marker,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,'w') as f:f.write(fingerprint)
mailbox=root/'.local/mailbox'
mailbox.mkdir(mode=0o700,exist_ok=True)
if mailbox.is_symlink() or mailbox.stat().st_mode & 0o077:
    raise SystemExit('Local mailbox must be a private directory')
print('Local account configuration prepared (key hidden)')

# Numeric ownership is local runtime metadata, never a credential.
current=dict(line.split('=',1) for line in env.read_text().splitlines())
current['MAILBOX_UID']=str(mailbox.stat().st_uid)
current['MAILBOX_GID']=str(mailbox.stat().st_gid)
fd,name=tempfile.mkstemp(prefix='.account-env-',dir=env.parent)
try:
    with os.fdopen(fd,'w') as f:
        f.write(''.join(k+'='+v+'\n' for k,v in current.items()))
        f.flush()
        os.fsync(f.fileno())
    os.replace(name,env)
finally:
    if os.path.exists(name):os.unlink(name)
