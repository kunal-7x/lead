export type Role =
  | 'super_admin'
  | 'internal_admin'
  | 'client_owner'
  | 'sales_manager'
  | 'salesperson'
  | 'viewer';

const NAV_PERMISSIONS: Record<string, Role[]> = {
  '/dashboard': [
    'super_admin',
    'internal_admin',
    'client_owner',
    'sales_manager',
    'salesperson',
    'viewer',
  ],
  '/leads': [
    'super_admin',
    'internal_admin',
    'client_owner',
    'sales_manager',
    'salesperson',
    'viewer',
  ],
  '/messages': ['super_admin', 'internal_admin', 'client_owner', 'sales_manager', 'salesperson'],
  '/messages/templates': ['super_admin', 'internal_admin', 'client_owner', 'sales_manager'],
  '/site-visits': ['super_admin', 'internal_admin', 'client_owner', 'sales_manager', 'salesperson'],
  '/site-visits/no-shows': [
    'super_admin',
    'internal_admin',
    'client_owner',
    'sales_manager',
    'salesperson',
  ],
  '/billing/usage': ['super_admin', 'internal_admin', 'client_owner', 'sales_manager'],
  '/billing/caps': ['super_admin', 'internal_admin', 'client_owner'],
  '/billing/invoices': ['super_admin', 'internal_admin', 'client_owner'],
  '/billing/credits': ['super_admin', 'internal_admin', 'client_owner'],
  '/reports': ['super_admin', 'internal_admin', 'client_owner', 'sales_manager'],
  '/settings/profile': [
    'super_admin',
    'internal_admin',
    'client_owner',
    'sales_manager',
    'salesperson',
    'viewer',
  ],
  '/settings/team': ['super_admin', 'internal_admin', 'client_owner', 'sales_manager'],
  '/settings/api-keys': ['super_admin', 'internal_admin', 'client_owner'],
  '/settings/audit-log': ['super_admin', 'internal_admin', 'client_owner'],
};

export function canAccess(path: string, roles: Role[]): boolean {
  const allowed = NAV_PERMISSIONS[path];
  if (!allowed) return true;
  return roles.some((r) => allowed.includes(r));
}
