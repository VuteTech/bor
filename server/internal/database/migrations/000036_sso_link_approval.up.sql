-- An administrator account is linked to a single sign-on identity by email
-- only after an administrator approved it; the approval expires.
ALTER TABLE users ADD COLUMN sso_link_allowed_until TIMESTAMPTZ NULL;
