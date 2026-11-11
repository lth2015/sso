# 完整 API 参考

OpenAPI 0.13.0；公共 Bearer API 共 43 个业务操作。此文件由 OpenAPI 生成，修改合同后运行 `scripts/docs build`。

真实交互入口见 [Playground 手册](playground.md)；外部接入见 [公共 API 手册](runbooks/public-api.md)。JSON/YAML 合同见 [OpenAPI](../api/openapi.yaml)。

示例 ID 和 proof 只表示格式，不是有效资源或登录材料。API 的 authority 来自真实 OAuth code+PKCE、已验证账户、MFA、会话与租户资格。写权限不隐含读权限；所有写入需最近 300 秒 MFA。

## 操作目录

| 方法 | 路径 | operationId |
| --- | --- | --- |
| POST | `/v1/invitations/accept` | `acceptInvitation` |
| GET | `/v1/me` | `getMe` |
| GET | `/v1/me/security` | `getMySecurity` |
| GET | `/v1/me/security-events` | `listMySecurityEvents` |
| GET | `/v1/sessions` | `listMySessions` |
| POST | `/v1/sessions/revoke-others` | `revokeOtherSessions` |
| POST | `/v1/sessions/{session}/revoke` | `revokeMySession` |
| GET | `/v1/tenants` | `listMyTenants` |
| POST | `/v1/tenants` | `createTenant` |
| GET | `/v1/tenants/{tenant}` | `getTenant` |
| GET | `/v1/tenants/{tenant}/applications` | `listTenantApplications` |
| POST | `/v1/tenants/{tenant}/applications` | `createApplication` |
| GET | `/v1/tenants/{tenant}/applications/{application}` | `getTenantApplication` |
| GET | `/v1/tenants/{tenant}/applications/{application}/clients` | `listApplicationClients` |
| POST | `/v1/tenants/{tenant}/applications/{application}/clients` | `createManagedClient` |
| GET | `/v1/tenants/{tenant}/applications/{application}/clients/{client}` | `getManagedClient` |
| POST | `/v1/tenants/{tenant}/applications/{application}/clients/{client}/reconcile` | `reconcileManagedClient` |
| POST | `/v1/tenants/{tenant}/applications/{application}/clients/{client}/revoke` | `revokeManagedClient` |
| POST | `/v1/tenants/{tenant}/applications/{application}/disable` | `disableTenantApplication` |
| POST | `/v1/tenants/{tenant}/applications/{application}/enable` | `enableTenantApplication` |
| GET | `/v1/tenants/{tenant}/audit-events` | `listAuditEvents` |
| GET | `/v1/tenants/{tenant}/invitations` | `listInvitations` |
| POST | `/v1/tenants/{tenant}/invitations` | `createInvitation` |
| POST | `/v1/tenants/{tenant}/invitations/{invitation}/revoke` | `revokeInvitation` |
| GET | `/v1/tenants/{tenant}/members` | `listMembers` |
| POST | `/v1/tenants/{tenant}/members/{member}/restore` | `restoreMember` |
| GET | `/v1/tenants/{tenant}/members/{member}/roles` | `listMemberRoles` |
| PUT | `/v1/tenants/{tenant}/members/{member}/roles/{role}` | `bindRole` |
| DELETE | `/v1/tenants/{tenant}/members/{member}/roles/{role}` | `unbindRole` |
| POST | `/v1/tenants/{tenant}/members/{member}/suspend` | `suspendMember` |
| GET | `/v1/tenants/{tenant}/organizations` | `listOrganizations` |
| POST | `/v1/tenants/{tenant}/organizations` | `createOrganization` |
| DELETE | `/v1/tenants/{tenant}/organizations/{org}` | `deleteOrganization` |
| GET | `/v1/tenants/{tenant}/organizations/{org}/members` | `listOrganizationMembers` |
| PUT | `/v1/tenants/{tenant}/organizations/{org}/members/{member}` | `assignOrganization` |
| DELETE | `/v1/tenants/{tenant}/organizations/{org}/members/{member}` | `unassignOrganization` |
| POST | `/v1/tenants/{tenant}/organizations/{org}/move` | `moveOrganization` |
| POST | `/v1/tenants/{tenant}/organizations/{org}/rename` | `renameOrganization` |
| GET | `/v1/tenants/{tenant}/permissions` | `getMyPermissions` |
| GET | `/v1/tenants/{tenant}/roles` | `listRoles` |
| POST | `/v1/tenants/{tenant}/roles` | `createRole` |
| PUT | `/v1/tenants/{tenant}/roles/{role}` | `replaceRole` |
| DELETE | `/v1/tenants/{tenant}/roles/{role}` | `deleteRole` |

## POST /v1/invitations/accept

Consume an invitation for the verified user

Recent MFA required. The proof selects the destination tenant. Verified email must match; membership only, no implicit role. Consumed/expired/revoked proofs are invalid. Acceptance increments destination tenant authorization version.

OAuth scope：`platform:write`。权限：`当前有效 User/Session/grant`。

### 参数

```json
[]
```

### 请求示例

```json
{
  "token": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [
    "token"
  ],
  "properties": {
    "token": {
      "type": "string",
      "pattern": "^[A-Za-z0-9_-]{43}$",
      "writeOnly": true
    }
  }
}
```

### 成功响应

HTTP 201

```json
{
  "description": "Consume an invitation for the verified user",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "membership_id"
        ],
        "properties": {
          "membership_id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/me

Get the verified current principal

Requires current User, MFA session and tenant-bound grant.

OAuth scope：`platform:read`。权限：`当前有效 User/Session/grant`。

### 参数

```json
[]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "Get the verified current principal",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "user_id",
          "tenant_id",
          "membership_id",
          "session_id"
        ],
        "properties": {
          "user_id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "tenant_id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "membership_id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "session_id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/me/security

Read own MFA/recovery/session metadata

Current verified user only, across their spaces. Counts and authentication metadata only; no factor secret, code digest, cookie or recovery code. Requires an active current tenant grant. Recovery-code regeneration is a separate trusted-origin password+TOTP interaction, never a Bearer API.

OAuth scope：`platform:read`。权限：`当前有效 User/Session/grant`。

### 参数

```json
[]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "Read own MFA/recovery/session metadata",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "mfa_enabled",
          "recovery_codes_remaining",
          "active_sessions",
          "authenticated_at",
          "second_factor"
        ],
        "properties": {
          "mfa_enabled": {
            "type": "boolean"
          },
          "recovery_codes_remaining": {
            "type": "integer",
            "minimum": 0,
            "maximum": 10
          },
          "active_sessions": {
            "type": "integer",
            "minimum": 0
          },
          "authenticated_at": {
            "type": "integer"
          },
          "second_factor": {
            "enum": [
              "otp",
              "recovery"
            ]
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/me/security-events

List own security events newest first

Current User only, across spaces; platform:read and a valid current tenant grant required, no tenant audit permission. Returns only reviewed event ID, action and timestamp; excludes internal targets, email, password, MFA proofs, IP and payloads. Order: occurred_at DESC, id DESC. after is the last event ID from this same user; unknown, hidden or another user cursor returns 404. Same-timestamp events use descending ID as a stable tie-breaker. No pagination snapshot, immutable-archive or complete security-monitoring guarantee. Reset-request events record receipt of a request, not proof that the user initiated it.

OAuth scope：`platform:read`。权限：`当前有效 User/Session/grant`。

### 参数

```json
[
  {
    "in": "query",
    "name": "limit",
    "description": "Bounded keyset page size.",
    "schema": {
      "type": "integer",
      "minimum": 1,
      "maximum": 100,
      "default": 50
    }
  },
  {
    "in": "query",
    "name": "after",
    "description": "Previous next_cursor; ascending UUID keyset, not a snapshot or timestamp ordering guarantee.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "List own security events newest first",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "items",
          "next_cursor"
        ],
        "properties": {
          "items": {
            "type": "array",
            "maxItems": 100,
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": [
                "id",
                "action",
                "occurred_at"
              ],
              "properties": {
                "id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "action": {
                  "type": "string",
                  "pattern": "^[a-z][a-z0-9_.]{0,63}$",
                  "description": "Reviewed security event code; never includes a private target/challenge ID or arbitrary payload."
                },
                "occurred_at": {
                  "type": "string",
                  "format": "date-time"
                }
              }
            }
          },
          "next_cursor": {
            "type": [
              "string",
              "null"
            ],
            "pattern": "^[a-f0-9]{32}$"
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/sessions

List central sessions of the current user

Requires current User, MFA session and tenant-bound grant.

OAuth scope：`platform:read`。权限：`当前有效 User/Session/grant`。

### 参数

```json
[
  {
    "in": "query",
    "name": "limit",
    "description": "Bounded keyset page size.",
    "schema": {
      "type": "integer",
      "minimum": 1,
      "maximum": 100,
      "default": 50
    }
  },
  {
    "in": "query",
    "name": "after",
    "description": "Previous next_cursor; ascending UUID keyset, not a snapshot or timestamp ordering guarantee.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "List central sessions of the current user",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "items",
          "next_cursor"
        ],
        "properties": {
          "items": {
            "type": "array",
            "maxItems": 100,
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": [
                "id",
                "current",
                "active",
                "authenticated_at",
                "second_factor",
                "idle_expires_at",
                "absolute_expires_at"
              ],
              "properties": {
                "id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "current": {
                  "type": "boolean"
                },
                "active": {
                  "type": "boolean"
                },
                "authenticated_at": {
                  "type": "integer"
                },
                "second_factor": {
                  "enum": [
                    "otp",
                    "recovery"
                  ]
                },
                "idle_expires_at": {
                  "type": "integer"
                },
                "absolute_expires_at": {
                  "type": "integer"
                }
              }
            }
          },
          "next_cursor": {
            "type": [
              "string",
              "null"
            ],
            "pattern": "^[a-f0-9]{32}$"
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/sessions/revoke-others

Revoke other central sessions, retaining the current session

Recent MFA required. No user ID input: only current user sessions, across their spaces and applications. Revokes other committed session records and their BFF/grant authority, queuing protocol cleanup in the same transaction. Current session/grants remain valid; authentication completing later may create a new session. No-op returns zero. Does not invalidate the current grant or update auth_time.

OAuth scope：`platform:write`。权限：`当前有效 User/Session/grant`。

### 参数

```json
[]
```

### 请求示例

```json
{}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [],
  "properties": {}
}
```

### 成功响应

HTTP 200

```json
{
  "description": "Revoke other central sessions, retaining the current session",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "revoked_sessions"
        ],
        "properties": {
          "revoked_sessions": {
            "type": "integer",
            "minimum": 0
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/sessions/{session}/revoke

Revoke one central session owned by the current user

Recent MFA required. Revokes business authority before queued Hydra cleanup. Revoking the current session invalidates this access token. Session cookie digests, CSRF values, factor secrets and tokens are never returned.

OAuth scope：`platform:write`。权限：`当前有效 User/Session/grant`。

### 参数

```json
[
  {
    "in": "path",
    "name": "session",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

### 请求示例

```json
{}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [],
  "properties": {}
}
```

### 成功响应

HTTP 204

```json
{
  "description": "Revoke one central session owned by the current user",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/tenants

List active spaces of the current user

Requires current User, MFA session and tenant-bound grant.

OAuth scope：`platform:read`。权限：`当前有效 User/Session/grant`。

### 参数

```json
[
  {
    "in": "query",
    "name": "limit",
    "description": "Bounded keyset page size.",
    "schema": {
      "type": "integer",
      "minimum": 1,
      "maximum": 100,
      "default": 50
    }
  },
  {
    "in": "query",
    "name": "after",
    "description": "Previous next_cursor; ascending UUID keyset, not a snapshot or timestamp ordering guarantee.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "List active spaces of the current user",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "items",
          "next_cursor"
        ],
        "properties": {
          "items": {
            "type": "array",
            "maxItems": 100,
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": [
                "id",
                "name",
                "slug",
                "membership_id",
                "owner",
                "authz_version",
                "org_version",
                "root_id"
              ],
              "properties": {
                "id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "name": {
                  "type": "string"
                },
                "slug": {
                  "type": "string",
                  "pattern": "^[a-z0-9][a-z0-9-]{0,62}$"
                },
                "membership_id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "owner": {
                  "type": "boolean"
                },
                "authz_version": {
                  "type": "integer",
                  "minimum": 1
                },
                "org_version": {
                  "type": "integer",
                  "minimum": 1
                },
                "root_id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                }
              }
            }
          },
          "next_cursor": {
            "type": [
              "string",
              "null"
            ],
            "pattern": "^[a-f0-9]{32}$"
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/tenants

Create a space and initial owner atomically

Recent MFA required. Maximum 20 active owned spaces. The first-party platform is enabled in the new space; unrelated applications are not enabled. Duplicate slug is 409; no automatic retry or idempotency-key guarantee.

OAuth scope：`platform:write`。权限：`当前有效 User/Session/grant`。

### 参数

```json
[]
```

### 请求示例

```json
{
  "slug": "example-team",
  "name": "示例团队"
}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [
    "slug",
    "name"
  ],
  "properties": {
    "slug": {
      "type": "string",
      "pattern": "^[a-z0-9][a-z0-9-]{0,62}$"
    },
    "name": {
      "type": "string",
      "minLength": 1,
      "maxLength": 128,
      "description": "Nonblank, trimmed, at most 128 UTF-8 bytes."
    }
  }
}
```

### 成功响应

HTTP 201

```json
{
  "description": "Create a space and initial owner atomically",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "Location": {
      "description": "Created tenant path; obtain a new tenant-bound authorization before using it.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "id",
          "membership_id",
          "root_id"
        ],
        "properties": {
          "id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "membership_id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "root_id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/tenants/{tenant}

Get the token-bound space and versions

Requires current User, MFA session and tenant-bound grant.

OAuth scope：`platform:read`。权限：`当前有效 User/Session/grant`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "Get the token-bound space and versions",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "id",
          "name",
          "slug",
          "membership_id",
          "owner",
          "authz_version",
          "org_version",
          "root_id"
        ],
        "properties": {
          "id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "name": {
            "type": "string"
          },
          "slug": {
            "type": "string",
            "pattern": "^[a-z0-9][a-z0-9-]{0,62}$"
          },
          "membership_id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "owner": {
            "type": "boolean"
          },
          "authz_version": {
            "type": "integer",
            "minimum": 1
          },
          "org_version": {
            "type": "integer",
            "minimum": 1
          },
          "root_id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/tenants/{tenant}/applications

List applications explicitly associated with this space

Active owner only. Includes disabled associations for recovery; no global application discovery, implicit enablement or secret access. The managed flag identifies this tenant ownership; separate owner-only creation handles owned apps.

OAuth scope：`platform:read`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "query",
    "name": "limit",
    "description": "Bounded keyset page size.",
    "schema": {
      "type": "integer",
      "minimum": 1,
      "maximum": 100,
      "default": 50
    }
  },
  {
    "in": "query",
    "name": "after",
    "description": "Previous next_cursor; ascending UUID keyset, not a snapshot or timestamp ordering guarantee.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "List applications explicitly associated with this space",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "items",
          "next_cursor"
        ],
        "properties": {
          "items": {
            "type": "array",
            "maxItems": 100,
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": [
                "id",
                "code",
                "name",
                "enabled",
                "available",
                "protected",
                "version",
                "client_count",
                "managed"
              ],
              "properties": {
                "id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "code": {
                  "type": "string"
                },
                "name": {
                  "type": "string"
                },
                "enabled": {
                  "type": "boolean",
                  "description": "Enabled for this token-bound tenant."
                },
                "available": {
                  "type": "boolean",
                  "description": "Global application registry is active; tenant owners cannot override global suspension."
                },
                "protected": {
                  "type": "boolean",
                  "description": "Platform management entry cannot be disabled through this API."
                },
                "version": {
                  "type": "integer",
                  "minimum": 1
                },
                "client_count": {
                  "type": "integer",
                  "minimum": 0
                },
                "managed": {
                  "type": "boolean",
                  "description": "Owned by this tenant; only such applications may register clients."
                }
              }
            }
          },
          "next_cursor": {
            "type": [
              "string",
              "null"
            ],
            "pattern": "^[a-f0-9]{32}$"
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/tenants/{tenant}/applications

Create a tenant-owned application

Active owner and recent MFA required. Creates application, this tenant association, immutable ownership and audit/outbox atomically. Maximum 20 owned apps per tenant. Other tenants and built-in/shared applications are not made owned by this call. No OAuth client or secret is issued; no unrelated tenant authorization version change.

OAuth scope：`platform:write`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

### 请求示例

```json
{
  "name": "本地客户端演示"
}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [
    "name"
  ],
  "properties": {
    "name": {
      "type": "string",
      "minLength": 1,
      "maxLength": 128,
      "description": "Trimmed nonblank display name, maximum 128 UTF-8 bytes. Code is generated, not a global namespace chosen by this caller."
    }
  }
}
```

### 成功响应

HTTP 201

```json
{
  "description": "Create a tenant-owned application",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    },
    "Location": {
      "description": "Created resource path; read metadata here after a result is known.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "id",
          "code",
          "name",
          "enabled",
          "available",
          "protected",
          "version",
          "client_count",
          "managed"
        ],
        "properties": {
          "id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "code": {
            "type": "string"
          },
          "name": {
            "type": "string"
          },
          "enabled": {
            "type": "boolean",
            "description": "Enabled for this token-bound tenant."
          },
          "available": {
            "type": "boolean",
            "description": "Global application registry is active; tenant owners cannot override global suspension."
          },
          "protected": {
            "type": "boolean",
            "description": "Platform management entry cannot be disabled through this API."
          },
          "version": {
            "type": "integer",
            "minimum": 1
          },
          "client_count": {
            "type": "integer",
            "minimum": 0
          },
          "managed": {
            "type": "boolean",
            "description": "Owned by this tenant; only such applications may register clients."
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/tenants/{tenant}/applications/{application}

Read an application association and resource-bound version

Active owner only. ETag binds tenant ID, application ID and association version. Global availability and per-tenant enablement are separate. Only existing associations may be managed.

OAuth scope：`platform:read`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "application",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "Read an application association and resource-bound version",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "id",
          "code",
          "name",
          "enabled",
          "available",
          "protected",
          "version",
          "client_count",
          "managed"
        ],
        "properties": {
          "id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "code": {
            "type": "string"
          },
          "name": {
            "type": "string"
          },
          "enabled": {
            "type": "boolean",
            "description": "Enabled for this token-bound tenant."
          },
          "available": {
            "type": "boolean",
            "description": "Global application registry is active; tenant owners cannot override global suspension."
          },
          "protected": {
            "type": "boolean",
            "description": "Platform management entry cannot be disabled through this API."
          },
          "version": {
            "type": "integer",
            "minimum": 1
          },
          "client_count": {
            "type": "integer",
            "minimum": 0
          },
          "managed": {
            "type": "boolean",
            "description": "Owned by this tenant; only such applications may register clients."
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/tenants/{tenant}/applications/{application}/clients

Read registered client metadata for an associated application

Active owner only, including disabled associations. Exact registered client_id, redirect URI, scopes, audience, active flag and version; never client secrets, cookies or token material. Cursor is printable client_id (not UUID), sorted ascending within this application. This GET does not change protocol configuration. managed/state distinguish tenant-owned lifecycle rows from external built-in registrations; no secret/authentication credential is returned.

OAuth scope：`platform:read`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "application",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "query",
    "name": "limit",
    "description": "Bounded keyset page size.",
    "schema": {
      "type": "integer",
      "minimum": 1,
      "maximum": 100,
      "default": 50
    }
  },
  {
    "in": "query",
    "name": "after",
    "description": "Previous client page next_cursor. Ascending printable client_id, not a UUID or pagination snapshot. URL-encode it.",
    "schema": {
      "type": "string",
      "minLength": 1,
      "maxLength": 128,
      "pattern": "^[!-~]{1,128}$"
    }
  }
]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "Read registered client metadata for an associated application",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "issuer",
          "items",
          "next_cursor"
        ],
        "properties": {
          "issuer": {
            "type": "string",
            "format": "uri"
          },
          "items": {
            "type": "array",
            "maxItems": 100,
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": [
                "client_id",
                "redirect_uri",
                "scopes",
                "audience",
                "active",
                "version",
                "managed",
                "state"
              ],
              "properties": {
                "client_id": {
                  "type": "string",
                  "minLength": 1,
                  "maxLength": 128
                },
                "redirect_uri": {
                  "type": "string",
                  "format": "uri"
                },
                "scopes": {
                  "type": "array",
                  "items": {
                    "type": "string"
                  }
                },
                "audience": {
                  "type": "string",
                  "format": "uri"
                },
                "active": {
                  "type": "boolean"
                },
                "version": {
                  "type": "integer",
                  "minimum": 1
                },
                "managed": {
                  "type": "boolean"
                },
                "state": {
                  "enum": [
                    "external",
                    "pending",
                    "active",
                    "revoked"
                  ]
                }
              }
            }
          },
          "next_cursor": {
            "type": [
              "string",
              "null"
            ],
            "minLength": 1,
            "maxLength": 128,
            "pattern": "^[!-~]{1,128}$"
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/tenants/{tenant}/applications/{application}/clients

Register a pending public PKCE client

Active owner/recent MFA; only applications owned by this tenant and currently available/enabled. Maximum 100 lifetime registrations per app, including revoked tombstones. Server chooses a random client_id. Starts pending and inactive, requiring separate reconcile to register and verify the engine. Fixed authorization_code/refresh_token, S256 PKCE, no client secret, fixed scopes openid offline_access platform:read and platform audience. No custom scopes/audience/grants/authentication/JWKS fields. Response loss: read application clients before creating another registration.

OAuth scope：`platform:write`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "application",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

### 请求示例

```json
{
  "name": "桌面客户端",
  "redirect_uri": "http://127.0.0.1:9876/callback"
}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [
    "name",
    "redirect_uri"
  ],
  "properties": {
    "name": {
      "type": "string",
      "minLength": 1,
      "maxLength": 128,
      "description": "Trimmed nonblank display name, maximum 128 UTF-8 bytes. Code is generated, not a global namespace chosen by this caller."
    },
    "redirect_uri": {
      "type": "string",
      "format": "uri",
      "maxLength": 255,
      "description": "Exact absolute HTTPS callback with path, or explicit HTTP IP loopback 127.0.0.1/[::1] on a fixed port 1024–65535. No wildcard, fragment, userinfo, backslash or whitespace. Never fetched by the platform."
    }
  }
}
```

### 成功响应

HTTP 201

```json
{
  "description": "Register a pending public PKCE client",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    },
    "Location": {
      "description": "Created resource path; read metadata here after a result is known.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "client_id",
          "application_id",
          "name",
          "redirect_uri",
          "scopes",
          "audience",
          "state",
          "active",
          "version"
        ],
        "properties": {
          "client_id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "application_id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "name": {
            "type": "string"
          },
          "redirect_uri": {
            "type": "string",
            "format": "uri"
          },
          "scopes": {
            "type": "array",
            "items": {
              "type": "string"
            }
          },
          "audience": {
            "type": "string",
            "const": "https://api.sso.local"
          },
          "state": {
            "enum": [
              "pending",
              "active",
              "revoked"
            ]
          },
          "active": {
            "type": "boolean"
          },
          "version": {
            "type": "integer",
            "minimum": 1
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/tenants/{tenant}/applications/{application}/clients/{client}

Read this tenant owned managed-client state and version

Active owner only. Database metadata without a protocol Admin fetch or secrets. client path is a server-generated 32-hex ID; existing shared/external registrations remain metadata-only via the application client list. Root application path and client must belong to this tenant ownership record.

OAuth scope：`platform:read`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "application",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "client",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "Read this tenant owned managed-client state and version",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "client_id",
          "application_id",
          "name",
          "redirect_uri",
          "scopes",
          "audience",
          "state",
          "active",
          "version"
        ],
        "properties": {
          "client_id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "application_id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "name": {
            "type": "string"
          },
          "redirect_uri": {
            "type": "string",
            "format": "uri"
          },
          "scopes": {
            "type": "array",
            "items": {
              "type": "string"
            }
          },
          "audience": {
            "type": "string",
            "const": "https://api.sso.local"
          },
          "state": {
            "enum": [
              "pending",
              "active",
              "revoked"
            ]
          },
          "active": {
            "type": "boolean"
          },
          "version": {
            "type": "integer",
            "minimum": 1
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/tenants/{tenant}/applications/{application}/clients/{client}/reconcile

Reconcile the exact owned protocol client profile

Active owner and recent MFA, exact ETag, total four-second authority transaction bound. Pending: read the fixed Admin destination, create if absent, read back and verify exact profile and ownership before activation/version/audit commit. Active: verify profile without changing authority. Revoked: delete only a matching owned engine object, retaining the revoked tombstone and auditing the cleanup check. Foreign/drifted profile is 409 protocol_client_conflict and never overwritten/deleted/adopted. Global/local app disable blocks activation, not revocation/cleanup. Unknown transport/commit outcome is not auto-replayed: reread state and choose a fresh reconciliation. Local pending/revoked clients cannot issue usable business tokens, including remote orphans. Absence is observed, not permanent physical-deletion assurance.

OAuth scope：`platform:write`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "application",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "client",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Exact managed-client detail ETag bound to tenant/client/version. Missing 428, malformed 400, stale/other resource 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"client-[a-f0-9]{32}-[a-f0-9]{32}-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [],
  "properties": {}
}
```

### 成功响应

HTTP 200

```json
{
  "description": "Reconcile the exact owned protocol client profile",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "client",
          "protocol_observation"
        ],
        "properties": {
          "client": {
            "type": "object",
            "additionalProperties": false,
            "required": [
              "client_id",
              "application_id",
              "name",
              "redirect_uri",
              "scopes",
              "audience",
              "state",
              "active",
              "version"
            ],
            "properties": {
              "client_id": {
                "type": "string",
                "pattern": "^[a-f0-9]{32}$"
              },
              "application_id": {
                "type": "string",
                "pattern": "^[a-f0-9]{32}$"
              },
              "name": {
                "type": "string"
              },
              "redirect_uri": {
                "type": "string",
                "format": "uri"
              },
              "scopes": {
                "type": "array",
                "items": {
                  "type": "string"
                }
              },
              "audience": {
                "type": "string",
                "const": "https://api.sso.local"
              },
              "state": {
                "enum": [
                  "pending",
                  "active",
                  "revoked"
                ]
              },
              "active": {
                "type": "boolean"
              },
              "version": {
                "type": "integer",
                "minimum": 1
              }
            }
          },
          "protocol_observation": {
            "enum": [
              "present",
              "absent"
            ],
            "description": "Observation during this bounded reconciliation, not a permanent guarantee that a timed-out upstream transaction cannot finish later."
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/tenants/{tenant}/applications/{application}/clients/{client}/revoke

Irreversibly revoke a managed client in the authority database

Active owner and recent MFA. First commits inactive and a higher client version with audit/outbox, invalidating this client grants immediately. No engine call or shared SID logout; other clients and central SSO remain valid. Pending or active can be revoked, never restored. Same-state/current ETag no-op. Separate reconcile can clean up an owned engine object; failed protocol cleanup never restores authority.

OAuth scope：`platform:write`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "application",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "client",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Exact managed-client detail ETag bound to tenant/client/version. Missing 428, malformed 400, stale/other resource 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"client-[a-f0-9]{32}-[a-f0-9]{32}-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [],
  "properties": {}
}
```

### 成功响应

HTTP 200

```json
{
  "description": "Irreversibly revoke a managed client in the authority database",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "client_id",
          "application_id",
          "name",
          "redirect_uri",
          "scopes",
          "audience",
          "state",
          "active",
          "version"
        ],
        "properties": {
          "client_id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "application_id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "name": {
            "type": "string"
          },
          "redirect_uri": {
            "type": "string",
            "format": "uri"
          },
          "scopes": {
            "type": "array",
            "items": {
              "type": "string"
            }
          },
          "audience": {
            "type": "string",
            "const": "https://api.sso.local"
          },
          "state": {
            "enum": [
              "pending",
              "active",
              "revoked"
            ]
          },
          "active": {
            "type": "boolean"
          },
          "version": {
            "type": "integer",
            "minimum": 1
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/tenants/{tenant}/applications/{application}/disable

Disable an existing application association

Active owner and recent MFA required. Exact tenant/application/version ETag. Atomically changes only this association version, records audit/outbox; disabling revokes this tenant/application grants and BFF sessions. Other spaces, applications and central SSO remain valid; no shared Hydra SID logout. Re-enabling requires new authorization and never resurrects old grants. Same-state requests with current ETag are no-ops. Management platform disable is 409 protected_application; globally suspended application enable is 409 application_unavailable. No registration or global registry mutation.

OAuth scope：`platform:write`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "application",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Exact application detail ETag, bound to tenant/application/version. Missing: 428; wrong resource or stale: 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"app-[a-f0-9]{32}-[a-f0-9]{32}-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [],
  "properties": {}
}
```

### 成功响应

HTTP 200

```json
{
  "description": "Disable an existing application association",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "id",
          "code",
          "name",
          "enabled",
          "available",
          "protected",
          "version",
          "client_count",
          "managed"
        ],
        "properties": {
          "id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "code": {
            "type": "string"
          },
          "name": {
            "type": "string"
          },
          "enabled": {
            "type": "boolean",
            "description": "Enabled for this token-bound tenant."
          },
          "available": {
            "type": "boolean",
            "description": "Global application registry is active; tenant owners cannot override global suspension."
          },
          "protected": {
            "type": "boolean",
            "description": "Platform management entry cannot be disabled through this API."
          },
          "version": {
            "type": "integer",
            "minimum": 1
          },
          "client_count": {
            "type": "integer",
            "minimum": 0
          },
          "managed": {
            "type": "boolean",
            "description": "Owned by this tenant; only such applications may register clients."
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/tenants/{tenant}/applications/{application}/enable

Enable an existing application association

Active owner and recent MFA required. Exact tenant/application/version ETag. Atomically changes only this association version, records audit/outbox; disabling revokes this tenant/application grants and BFF sessions. Other spaces, applications and central SSO remain valid; no shared Hydra SID logout. Re-enabling requires new authorization and never resurrects old grants. Same-state requests with current ETag are no-ops. Management platform disable is 409 protected_application; globally suspended application enable is 409 application_unavailable. No registration or global registry mutation.

OAuth scope：`platform:write`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "application",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Exact application detail ETag, bound to tenant/application/version. Missing: 428; wrong resource or stale: 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"app-[a-f0-9]{32}-[a-f0-9]{32}-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [],
  "properties": {}
}
```

### 成功响应

HTTP 200

```json
{
  "description": "Enable an existing application association",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "id",
          "code",
          "name",
          "enabled",
          "available",
          "protected",
          "version",
          "client_count",
          "managed"
        ],
        "properties": {
          "id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "code": {
            "type": "string"
          },
          "name": {
            "type": "string"
          },
          "enabled": {
            "type": "boolean",
            "description": "Enabled for this token-bound tenant."
          },
          "available": {
            "type": "boolean",
            "description": "Global application registry is active; tenant owners cannot override global suspension."
          },
          "protected": {
            "type": "boolean",
            "description": "Platform management entry cannot be disabled through this API."
          },
          "version": {
            "type": "integer",
            "minimum": 1
          },
          "client_count": {
            "type": "integer",
            "minimum": 0
          },
          "managed": {
            "type": "boolean",
            "description": "Owned by this tenant; only such applications may register clients."
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/tenants/{tenant}/audit-events

List controlled space audit metadata

Requires audit.read on the token tenant. Explicit deny overrides other allows, including owner operational privileges.

OAuth scope：`platform:read`。权限：`permission:audit.read`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "query",
    "name": "limit",
    "description": "Bounded keyset page size.",
    "schema": {
      "type": "integer",
      "minimum": 1,
      "maximum": 100,
      "default": 50
    }
  },
  {
    "in": "query",
    "name": "after",
    "description": "Previous next_cursor; ascending UUID keyset, not a snapshot or timestamp ordering guarantee.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "List controlled space audit metadata",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "items",
          "next_cursor"
        ],
        "properties": {
          "items": {
            "type": "array",
            "maxItems": 100,
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": [
                "id",
                "actor_membership_id",
                "action",
                "target_id",
                "occurred_at"
              ],
              "properties": {
                "id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "actor_membership_id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "action": {
                  "type": "string"
                },
                "target_id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "occurred_at": {
                  "type": "string",
                  "format": "date-time"
                },
                "role_id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "membership_id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "role_code": {
                  "type": "string"
                }
              }
            }
          },
          "next_cursor": {
            "type": [
              "string",
              "null"
            ],
            "pattern": "^[a-f0-9]{32}$"
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/tenants/{tenant}/invitations

List invitation metadata without bearer secrets

Requires invitations.manage on the token tenant. Explicit deny overrides other allows, including owner operational privileges.

OAuth scope：`platform:read`。权限：`permission:invitations.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "query",
    "name": "limit",
    "description": "Bounded keyset page size.",
    "schema": {
      "type": "integer",
      "minimum": 1,
      "maximum": 100,
      "default": 50
    }
  },
  {
    "in": "query",
    "name": "after",
    "description": "Previous next_cursor; ascending UUID keyset, not a snapshot or timestamp ordering guarantee.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "List invitation metadata without bearer secrets",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "items",
          "next_cursor"
        ],
        "properties": {
          "items": {
            "type": "array",
            "maxItems": 100,
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": [
                "id",
                "email",
                "expires_at",
                "state"
              ],
              "properties": {
                "id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "email": {
                  "type": "string",
                  "format": "email"
                },
                "expires_at": {
                  "type": "integer"
                },
                "state": {
                  "enum": [
                    "pending",
                    "expired",
                    "closed"
                  ],
                  "description": "Closed covers accepted or revoked; audit events distinguish the transition."
                }
              }
            }
          },
          "next_cursor": {
            "type": [
              "string",
              "null"
            ],
            "pattern": "^[a-f0-9]{32}$"
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/tenants/{tenant}/invitations

Create an email-bound one-time invitation

Requires invitations.manage on the token tenant. Explicit deny overrides other allows, including owner operational privileges. Recent MFA required. Token is returned once, stored only as a digest and never in audit/outbox. The caller delivers it securely; no external email is sent. Invitations grant membership only.

OAuth scope：`platform:write`。权限：`permission:invitations.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

### 请求示例

```json
{
  "email": "teammate@example.invalid"
}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [
    "email"
  ],
  "properties": {
    "email": {
      "type": "string",
      "format": "email",
      "maxLength": 254
    }
  }
}
```

### 成功响应

HTTP 201

```json
{
  "description": "Create an email-bound one-time invitation",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "id",
          "token",
          "expires_in"
        ],
        "properties": {
          "id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "token": {
            "type": "string",
            "pattern": "^[A-Za-z0-9_-]{43}$",
            "readOnly": true,
            "description": "One-time bearer invitation returned only here; keep out of logs. No automatic email transport."
          },
          "expires_in": {
            "type": "integer",
            "const": 86400
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/tenants/{tenant}/invitations/{invitation}/revoke

Close an invitation so its proof cannot be accepted

Requires invitations.manage on the token tenant. Explicit deny overrides other allows, including owner operational privileges. Recent MFA required. Idempotently closes the proof; already closed proofs stay closed. Audit records the first revocation; closed includes accepted or revoked.

OAuth scope：`platform:write`。权限：`permission:invitations.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "invitation",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

### 请求示例

```json
{}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [],
  "properties": {}
}
```

### 成功响应

HTTP 204

```json
{
  "description": "Close an invitation so its proof cannot be accepted",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/tenants/{tenant}/members

List space memberships

Requires members.read on the token tenant. Explicit deny overrides other allows, including owner operational privileges.

OAuth scope：`platform:read`。权限：`permission:members.read`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "query",
    "name": "limit",
    "description": "Bounded keyset page size.",
    "schema": {
      "type": "integer",
      "minimum": 1,
      "maximum": 100,
      "default": 50
    }
  },
  {
    "in": "query",
    "name": "after",
    "description": "Previous next_cursor; ascending UUID keyset, not a snapshot or timestamp ordering guarantee.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "List space memberships",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "items",
          "next_cursor"
        ],
        "properties": {
          "items": {
            "type": "array",
            "maxItems": 100,
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": [
                "id",
                "user_id",
                "email",
                "status",
                "owner"
              ],
              "properties": {
                "id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "user_id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "email": {
                  "type": [
                    "string",
                    "null"
                  ],
                  "format": "email",
                  "description": "Verified alias requires members.read on the token tenant."
                },
                "status": {
                  "enum": [
                    "active",
                    "invited",
                    "suspended",
                    "left"
                  ]
                },
                "owner": {
                  "type": "boolean"
                }
              }
            }
          },
          "next_cursor": {
            "type": [
              "string",
              "null"
            ],
            "pattern": "^[a-f0-9]{32}$"
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/tenants/{tenant}/members/{member}/restore

Restore a membership

Requires members.write on the token tenant. Explicit deny overrides other allows, including owner operational privileges. Recent MFA required. Last active owner cannot be suspended. Changes increment membership and tenant versions, invalidating existing tenant grants; restoration never revives old grants.

OAuth scope：`platform:write`。权限：`permission:members.write`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "member",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Latest tenant/members ETag. Missing: 428; stale: 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"authz-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [],
  "properties": {}
}
```

### 成功响应

HTTP 204

```json
{
  "description": "Restore a membership",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/tenants/{tenant}/members/{member}/roles

List one tenant membership role bindings

Active tenant owner required.

OAuth scope：`platform:read`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "member",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "List one tenant membership role bindings",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "items"
        ],
        "properties": {
          "items": {
            "type": "array",
            "maxItems": 100,
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": [
                "id",
                "code",
                "built_in",
                "scope",
                "policies",
                "binding_count"
              ],
              "properties": {
                "id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "code": {
                  "type": "string"
                },
                "built_in": {
                  "type": "boolean"
                },
                "scope": {
                  "const": "tenant"
                },
                "policies": {
                  "type": "array",
                  "items": {
                    "type": "object",
                    "additionalProperties": false,
                    "required": [
                      "permission",
                      "effect"
                    ],
                    "properties": {
                      "permission": {
                        "enum": [
                          "audit.read",
                          "invitations.manage",
                          "members.read",
                          "members.write",
                          "organizations.read",
                          "organizations.write"
                        ]
                      },
                      "effect": {
                        "enum": [
                          "allow",
                          "deny"
                        ]
                      }
                    }
                  }
                },
                "binding_count": {
                  "type": "integer",
                  "minimum": 0
                }
              }
            }
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## PUT /v1/tenants/{tenant}/members/{member}/roles/{role}

Bind a tenant role to a membership

Owner and recent MFA required. Both objects must belong to the token tenant. Owner grants/revocations are explicit reserved-role operations; last active owner cannot be removed. Repeating the same binding state with the current ETag is a no-op. A real change increments tenant authz_version; old grants must reauthorize. Audit preserves member and role metadata even after role deletion.

OAuth scope：`platform:write`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "member",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "role",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Latest tenant/members ETag. Missing: 428; stale: 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"authz-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [],
  "properties": {}
}
```

### 成功响应

HTTP 204

```json
{
  "description": "Bind a tenant role to a membership",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## DELETE /v1/tenants/{tenant}/members/{member}/roles/{role}

Unbind a tenant role to a membership

Owner and recent MFA required. Both objects must belong to the token tenant. Owner grants/revocations are explicit reserved-role operations; last active owner cannot be removed. Repeating the same binding state with the current ETag is a no-op. A real change increments tenant authz_version; old grants must reauthorize. Audit preserves member and role metadata even after role deletion.

OAuth scope：`platform:write`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "member",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "role",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Latest tenant/members ETag. Missing: 428; stale: 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"authz-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [],
  "properties": {}
}
```

### 成功响应

HTTP 204

```json
{
  "description": "Unbind a tenant role to a membership",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/tenants/{tenant}/members/{member}/suspend

Suspend a membership

Requires members.write on the token tenant. Explicit deny overrides other allows, including owner operational privileges. Recent MFA required. Last active owner cannot be suspended. Changes increment membership and tenant versions, invalidating existing tenant grants; restoration never revives old grants.

OAuth scope：`platform:write`。权限：`permission:members.write`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "member",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Latest tenant/members ETag. Missing: 428; stale: 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"authz-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [],
  "properties": {}
}
```

### 成功响应

HTTP 204

```json
{
  "description": "Suspend a membership",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/tenants/{tenant}/organizations

List space organization nodes

Requires organizations.read on the token tenant. Explicit deny overrides other allows, including owner operational privileges.

OAuth scope：`platform:read`。权限：`permission:organizations.read`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "query",
    "name": "limit",
    "description": "Bounded keyset page size.",
    "schema": {
      "type": "integer",
      "minimum": 1,
      "maximum": 100,
      "default": 50
    }
  },
  {
    "in": "query",
    "name": "after",
    "description": "Previous next_cursor; ascending UUID keyset, not a snapshot or timestamp ordering guarantee.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "List space organization nodes",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "items",
          "next_cursor"
        ],
        "properties": {
          "items": {
            "type": "array",
            "maxItems": 100,
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": [
                "id",
                "parent_id",
                "name"
              ],
              "properties": {
                "id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "parent_id": {
                  "type": [
                    "string",
                    "null"
                  ],
                  "pattern": "^[a-f0-9]{32}$"
                },
                "name": {
                  "type": "string"
                }
              }
            }
          },
          "next_cursor": {
            "type": [
              "string",
              "null"
            ],
            "pattern": "^[a-f0-9]{32}$"
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/tenants/{tenant}/organizations

Create an organization under an existing parent

Requires organizations.write on the token tenant. Explicit deny overrides other allows, including owner operational privileges.

OAuth scope：`platform:write`。权限：`permission:organizations.write`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Latest organizations ETag. Missing: 428; stale: 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"org-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{
  "parent_id": "00000000000000000000000000000000",
  "name": "示例组织"
}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [
    "parent_id",
    "name"
  ],
  "properties": {
    "parent_id": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    },
    "name": {
      "type": "string",
      "minLength": 1,
      "maxLength": 128,
      "description": "Nonblank, trimmed, at most 128 UTF-8 bytes."
    }
  }
}
```

### 成功响应

HTTP 201

```json
{
  "description": "Create an organization under an existing parent",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "id",
          "org_version"
        ],
        "properties": {
          "id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "org_version": {
            "type": "integer",
            "minimum": 1
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## DELETE /v1/tenants/{tenant}/organizations/{org}

Delete an empty non-root organization

Requires organizations.write on the token tenant. Explicit deny overrides other allows, including owner operational privileges. Recent MFA required. No cascade. Root is 409 protected_organization. Any direct child or direct member association, including suspended/left members, is 409 organization_not_empty. Tenant lock serializes with create/move/assign. Removes closure references, increments org_version and tenant authz_version with audit/outbox atomically; old tenant grants must reauthorize. Missing/already deleted returns 404. Network result unknown: read before deciding to retry.

OAuth scope：`platform:write`。权限：`permission:organizations.write`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "org",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Latest organizations ETag. Missing: 428; stale: 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"org-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [],
  "properties": {}
}
```

### 成功响应

HTTP 200

```json
{
  "description": "Delete an empty non-root organization",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "id",
          "org_version"
        ],
        "properties": {
          "id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "org_version": {
            "type": "integer",
            "minimum": 1
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/tenants/{tenant}/organizations/{org}/members

List direct organization member associations

Requires organizations.read+members.read on the token tenant. Explicit deny overrides other allows, including owner operational privileges. Requires BOTH organizations.read and members.read, explicit deny wins. Includes inactive memberships so stale associations can be removed. Direct associations only, not descendants or effective permissions. Verified email has the same members.read protection as the space member directory. Returns authz ETag for removal. UUID keyset pagination, no snapshot.

OAuth scope：`platform:read`。权限：`permission:organizations.read+members.read`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "org",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "query",
    "name": "limit",
    "description": "Bounded keyset page size.",
    "schema": {
      "type": "integer",
      "minimum": 1,
      "maximum": 100,
      "default": 50
    }
  },
  {
    "in": "query",
    "name": "after",
    "description": "Previous next_cursor; ascending UUID keyset, not a snapshot or timestamp ordering guarantee.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "List direct organization member associations",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "items",
          "next_cursor"
        ],
        "properties": {
          "items": {
            "type": "array",
            "maxItems": 100,
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": [
                "id",
                "user_id",
                "email",
                "status",
                "owner"
              ],
              "properties": {
                "id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "user_id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "email": {
                  "type": [
                    "string",
                    "null"
                  ],
                  "format": "email",
                  "description": "Verified alias requires members.read on the token tenant."
                },
                "status": {
                  "enum": [
                    "active",
                    "invited",
                    "suspended",
                    "left"
                  ]
                },
                "owner": {
                  "type": "boolean"
                }
              }
            }
          },
          "next_cursor": {
            "type": [
              "string",
              "null"
            ],
            "pattern": "^[a-f0-9]{32}$"
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## PUT /v1/tenants/{tenant}/organizations/{org}/members/{member}

Associate a membership with an organization

Requires organizations.write on the token tenant. Explicit deny overrides other allows, including owner operational privileges. Association does not grant owner or other roles. Changes tenant authorization version; obtain a new authorization afterwards.

OAuth scope：`platform:write`。权限：`permission:organizations.write`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "member",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "org",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Latest tenant/members ETag. Missing: 428; stale: 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"authz-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [],
  "properties": {}
}
```

### 成功响应

HTTP 204

```json
{
  "description": "Associate a membership with an organization",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## DELETE /v1/tenants/{tenant}/organizations/{org}/members/{member}

Remove one direct organization member association

Requires organizations.write on the token tenant. Explicit deny overrides other allows, including owner operational privileges. Recent MFA and organizations.write required. Both objects must belong to this tenant. Inactive members can be unassigned. Leaves membership status, owner/role bindings and other organization associations intact. A real removal increments tenant authz_version and records audit/outbox; old tenant grants must reauthorize. Already absent association with current ETag is a no-op; missing/foreign objects return 404. Organization assignment does not itself grant access.

OAuth scope：`platform:write`。权限：`permission:organizations.write`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "member",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "org",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Latest tenant/members ETag. Missing: 428; stale: 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"authz-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [],
  "properties": {}
}
```

### 成功响应

HTTP 204

```json
{
  "description": "Remove one direct organization member association",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/tenants/{tenant}/organizations/{org}/move

Preview or execute an organization move

Requires organizations.write on the token tenant. Explicit deny overrides other allows, including owner operational privileges. Recent MFA required, including preview. Root/cycle/depth checks apply. Preview changes no state. Executing changes organization and tenant authorization versions; obtain a new authorization afterwards.

OAuth scope：`platform:write`。权限：`permission:organizations.write`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "org",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Latest organizations ETag. Missing: 428; stale: 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"org-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{
  "parent_id": "00000000000000000000000000000000",
  "dry_run": true
}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [
    "parent_id",
    "dry_run"
  ],
  "properties": {
    "parent_id": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    },
    "dry_run": {
      "type": "boolean",
      "description": "Required; false executes the move and invalidates old tenant grants."
    }
  }
}
```

### 成功响应

HTTP 200

```json
{
  "description": "Preview or execute an organization move",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "subtree_nodes",
          "resulting_depth",
          "org_version",
          "dry_run"
        ],
        "properties": {
          "subtree_nodes": {
            "type": "integer",
            "minimum": 1
          },
          "resulting_depth": {
            "type": "integer",
            "minimum": 0,
            "maximum": 16
          },
          "org_version": {
            "type": "integer",
            "minimum": 1
          },
          "dry_run": {
            "type": "boolean"
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/tenants/{tenant}/organizations/{org}/rename

Rename a non-root organization

Requires organizations.write on the token tenant. Explicit deny overrides other allows, including owner operational privileges. Recent MFA required. Nonblank trimmed name, at most 128 UTF-8 bytes. Increments only org_version: names do not confer authority. Identical normalized name with current ETag is a no-op. Root is 409 protected_organization; changing the space name is outside organization delegation.

OAuth scope：`platform:write`。权限：`permission:organizations.write`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "org",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Latest organizations ETag. Missing: 428; stale: 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"org-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{
  "name": "新的组织名称"
}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [
    "name"
  ],
  "properties": {
    "name": {
      "type": "string",
      "minLength": 1,
      "maxLength": 128,
      "description": "Nonblank trimmed name, at most 128 UTF-8 bytes."
    }
  }
}
```

### 成功响应

HTTP 200

```json
{
  "description": "Rename a non-root organization",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "id",
          "org_version"
        ],
        "properties": {
          "id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "org_version": {
            "type": "integer",
            "minimum": 1
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/tenants/{tenant}/permissions

Get the current membership effective permissions

Any current member may inspect their own effective set. Explicit deny wins across tenant-scoped roles. Reserved owner roles.manage is retained for recovery and cannot be expressed in custom policies.

OAuth scope：`platform:read`。权限：`当前有效 User/Session/grant`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "Get the current membership effective permissions",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "permissions",
          "denied",
          "owner",
          "authz_version",
          "catalog"
        ],
        "properties": {
          "permissions": {
            "type": "array",
            "uniqueItems": true,
            "items": {
              "enum": [
                "audit.read",
                "invitations.manage",
                "members.read",
                "members.write",
                "organizations.read",
                "organizations.write",
                "roles.manage"
              ]
            }
          },
          "denied": {
            "type": "array",
            "uniqueItems": true,
            "items": {
              "enum": [
                "audit.read",
                "invitations.manage",
                "members.read",
                "members.write",
                "organizations.read",
                "organizations.write"
              ]
            }
          },
          "owner": {
            "type": "boolean"
          },
          "authz_version": {
            "type": "integer",
            "minimum": 1
          },
          "catalog": {
            "type": "array",
            "items": {
              "enum": [
                "audit.read",
                "invitations.manage",
                "members.read",
                "members.write",
                "organizations.read",
                "organizations.write"
              ]
            }
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## GET /v1/tenants/{tenant}/roles

List tenant roles and rules

Active tenant owner required.

OAuth scope：`platform:read`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "query",
    "name": "limit",
    "description": "Bounded keyset page size.",
    "schema": {
      "type": "integer",
      "minimum": 1,
      "maximum": 100,
      "default": 50
    }
  },
  {
    "in": "query",
    "name": "after",
    "description": "Previous next_cursor; ascending UUID keyset, not a snapshot or timestamp ordering guarantee.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  }
]
```

无请求体。

### 成功响应

HTTP 200

```json
{
  "description": "List tenant roles and rules",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "items",
          "next_cursor"
        ],
        "properties": {
          "items": {
            "type": "array",
            "maxItems": 100,
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": [
                "id",
                "code",
                "built_in",
                "scope",
                "policies",
                "binding_count"
              ],
              "properties": {
                "id": {
                  "type": "string",
                  "pattern": "^[a-f0-9]{32}$"
                },
                "code": {
                  "type": "string"
                },
                "built_in": {
                  "type": "boolean"
                },
                "scope": {
                  "const": "tenant"
                },
                "policies": {
                  "type": "array",
                  "items": {
                    "type": "object",
                    "additionalProperties": false,
                    "required": [
                      "permission",
                      "effect"
                    ],
                    "properties": {
                      "permission": {
                        "enum": [
                          "audit.read",
                          "invitations.manage",
                          "members.read",
                          "members.write",
                          "organizations.read",
                          "organizations.write"
                        ]
                      },
                      "effect": {
                        "enum": [
                          "allow",
                          "deny"
                        ]
                      }
                    }
                  }
                },
                "binding_count": {
                  "type": "integer",
                  "minimum": 0
                }
              }
            }
          },
          "next_cursor": {
            "type": [
              "string",
              "null"
            ],
            "pattern": "^[a-f0-9]{32}$"
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## POST /v1/tenants/{tenant}/roles

Create a tenant-scoped custom role

Active tenant owner required.

OAuth scope：`platform:write`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Latest tenant/members ETag. Missing: 428; stale: 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"authz-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{
  "code": "auditor",
  "policies": [
    {
      "permission": "audit.read",
      "effect": "allow"
    }
  ]
}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [
    "code",
    "policies"
  ],
  "properties": {
    "code": {
      "type": "string",
      "pattern": "^[a-z][a-z0-9_-]{0,62}$",
      "not": {
        "const": "owner"
      }
    },
    "policies": {
      "type": "array",
      "maxItems": 12,
      "uniqueItems": true,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "permission",
          "effect"
        ],
        "properties": {
          "permission": {
            "enum": [
              "audit.read",
              "invitations.manage",
              "members.read",
              "members.write",
              "organizations.read",
              "organizations.write"
            ]
          },
          "effect": {
            "enum": [
              "allow",
              "deny"
            ]
          }
        }
      }
    }
  }
}
```

### 成功响应

HTTP 201

```json
{
  "description": "Create a tenant-scoped custom role",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    },
    "ETag": {
      "description": "Quoted collection version for If-Match.",
      "schema": {
        "type": "string"
      }
    }
  },
  "content": {
    "application/json": {
      "schema": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "id",
          "authz_version"
        ],
        "properties": {
          "id": {
            "type": "string",
            "pattern": "^[a-f0-9]{32}$"
          },
          "authz_version": {
            "type": "integer",
            "minimum": 1
          }
        }
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## PUT /v1/tenants/{tenant}/roles/{role}

Replace all custom role rules

Owner and recent MFA required. Reserved owner is immutable. Duplicate rules and reserved permissions are invalid. Changed policies increment tenant authorization version and invalidate prior tenant grants; identical rules are a no-op.

OAuth scope：`platform:write`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "role",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Latest tenant/members ETag. Missing: 428; stale: 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"authz-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{
  "policies": [
    {
      "permission": "audit.read",
      "effect": "allow"
    }
  ]
}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [
    "policies"
  ],
  "properties": {
    "policies": {
      "type": "array",
      "maxItems": 12,
      "uniqueItems": true,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": [
          "permission",
          "effect"
        ],
        "properties": {
          "permission": {
            "enum": [
              "audit.read",
              "invitations.manage",
              "members.read",
              "members.write",
              "organizations.read",
              "organizations.write"
            ]
          },
          "effect": {
            "enum": [
              "allow",
              "deny"
            ]
          }
        }
      }
    }
  }
}
```

### 成功响应

HTTP 204

```json
{
  "description": "Replace all custom role rules",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## DELETE /v1/tenants/{tenant}/roles/{role}

Delete an unbound custom role

Owner and recent MFA required. Built-in owner cannot be deleted; roles with bindings return role_in_use. Changed state invalidates tenant grants.

OAuth scope：`platform:write`。权限：`reserved_owner:roles.manage`。

### 参数

```json
[
  {
    "in": "path",
    "name": "tenant",
    "required": true,
    "description": "Must equal the access token tenant; another tenant returns 404, even if the user also belongs to it.",
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "path",
    "name": "role",
    "required": true,
    "schema": {
      "type": "string",
      "pattern": "^[a-f0-9]{32}$"
    }
  },
  {
    "in": "header",
    "name": "If-Match",
    "required": true,
    "description": "Latest tenant/members ETag. Missing: 428; stale: 412.",
    "schema": {
      "type": "string",
      "pattern": "^\"authz-[1-9][0-9]*\"$"
    }
  }
]
```

### 请求示例

```json
{}
```

### 请求 schema

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [],
  "properties": {}
}
```

### 成功响应

HTTP 204

```json
{
  "description": "Delete an unbound custom role",
  "headers": {
    "X-Request-ID": {
      "description": "Server-generated request correlation ID.",
      "schema": {
        "type": "string"
      }
    }
  }
}
```

错误状态：400、401、403、404、409、412、413、415、428、429、503。错误 body 使用下文公共 Problem schema。


## 公共错误合同

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": [
    "type",
    "title",
    "status",
    "code",
    "request_id"
  ],
  "properties": {
    "type": {
      "type": "string",
      "format": "uri"
    },
    "title": {
      "type": "string"
    },
    "status": {
      "type": "integer",
      "minimum": 400,
      "maximum": 599
    },
    "code": {
      "type": "string"
    },
    "request_id": {
      "type": "string"
    }
  }
}
```

401：授权无效/已撤销；403：scope/权限不足或 recent_mfa_required；404：资源或当前用户的游标不存在；409：already_exists、last_active_owner、reserved_role、role_in_use、组织环/root 保护；412：版本冲突；428：缺少前置条件；429：限流，按 Retry-After 等待；503：权威服务暂不可用。

BFF 会话错误还可能返回 `{ "code": "session_unavailable" }`。两类边界合同不可混用。

## 请求与重试规则

body 上限 4096 字节；ID 使用 32 位小写十六进制。列表默认 50，limit 为 1–100；after 使用 next_cursor，不保证分页快照。请求仅接受 JSON、受控字段和有限策略数组。禁止额外 JSON 文档、重复键、超深结构和非法 UTF-8。

If-Match 使用带双引号的 authz-N / org-N。权限版本变更使旧 grant 失效，包括执行者；refresh 不更新权限或认证时间。重新授权后由调用者决定是否再次提交。没有通用 Idempotency-Key，创建或网络中断后须核对结果，不能盲目重试。
