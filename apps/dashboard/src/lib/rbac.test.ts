import { canAccess } from "./rbac";

test("salesperson cannot access /settings/team", () => {
  expect(canAccess("/settings/team", ["salesperson"])).toBe(false);
});

test("client_owner can access /settings/team", () => {
  expect(canAccess("/settings/team", ["client_owner"])).toBe(true);
});

test("viewer can access /dashboard", () => {
  expect(canAccess("/dashboard", ["viewer"])).toBe(true);
});

test("salesperson can access /settings/profile", () => {
  expect(canAccess("/settings/profile", ["salesperson"])).toBe(true);
});
