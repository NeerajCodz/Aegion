import type { Operator } from '../types';

function matchesPermission(granted: string, required: string): boolean {
  if (granted === required || granted === '*') {
    return true;
  }
  if (granted.endsWith(':*')) {
    return required.startsWith(granted.slice(0, -1));
  }
  return false;
}

export function operatorHasPermission(operator: Operator | null | undefined, required: string): boolean {
  if (!operator || !required) {
    return false;
  }

  const effective = operator.effective_permissions ?? [];
  if (effective.length > 0) {
    return effective.some((permission) => matchesPermission(permission, required));
  }

  const fallbackByRole: Record<string, string[]> = {
    super_admin: ['*'],
    admin: [
      'identities:*',
      'sessions:*',
      'config:read',
      'config:update',
      'audit:read',
      'operators:read',
      'roles:read',
      'oauth2:clients:read',
      'oauth2:clients:manage',
      'oauth2:tokens:read',
      'oauth2:tokens:revoke',
      'security:read',
      'security:create',
      'security:delete',
    ],
    operator: ['identities:read', 'identities:update', 'sessions:read', 'sessions:delete', 'audit:read', 'security:read'],
    viewer: [
      'identities:read',
      'sessions:read',
      'config:read',
      'audit:read',
      'oauth2:clients:read',
      'oauth2:tokens:read',
      'security:read',
    ],
  };
  const rolePermissions = fallbackByRole[operator.role.toLowerCase()] ?? [];
  return rolePermissions.some((permission) => matchesPermission(permission, required));
}
