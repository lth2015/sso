"""Validate real HTTP responses without printing sensitive instance data."""
import json,sys,yaml
from pathlib import Path
from jsonschema import Draft202012Validator,FormatChecker
try:
 sample=json.load(sys.stdin)
 rootpath=Path(__file__).resolve().parents[2]
 spec=yaml.safe_load((rootpath/'api/console.openapi.yaml').read_text()) if sample.get('contract')=='console' else json.loads((rootpath/'api/openapi.json').read_text())
 operation=spec['paths'][sample['path']][sample['method'].lower()]
 response=operation['responses'][str(sample['status'])]
 if '$ref' in response:
  response=spec['components']['responses'][response['$ref'].rsplit('/',1)[1]]
 if 'content' in response:
  media=sample.get('content_type','').split(';')[0]
  assert media in response['content']
  content=response['content'][media]
  if 'schema' in content:
   root={'$schema':'https://json-schema.org/draft/2020-12/schema','$ref':'#/response','response':content['schema'],'components':spec['components']}
   Draft202012Validator(root,format_checker=FormatChecker()).validate(sample['body'])
 if sample['status']<400 and 'requestBody' in operation:
  body_schema=operation['requestBody']['content']['application/json']['schema']
  root={'$schema':'https://json-schema.org/draft/2020-12/schema','$ref':'#/request','request':body_schema,'components':spec['components']}
  Draft202012Validator(root,format_checker=FormatChecker()).validate(sample['request_body'])
 if sample['status']>=400 and sample.get('contract')=='console' and sample['content_type'].startswith('application/json'):
  assert sample['body']=={'code':'session_unavailable'}
 elif sample['status']>=400:
  assert sample['content_type'].startswith('application/problem+json')
  assert sample['body']['status']==sample['status']
  assert sample['request_id']==sample['body']['request_id'] and len(sample['request_id'])==24
except Exception:
 raise SystemExit('Public API response contract validation failed (sensitive details withheld)')
