// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback } from "react";
import { LiveAlert } from "../../components/LiveAlert";
import { BorEmptyState } from "../../components/BorEmptyState";
import { ConfirmModal } from "../../components/ConfirmModal";
import { useToast } from "../../components/ToastHost";
import {
  Button,
  Spinner,
  Modal,
  ModalVariant,
  ModalHeader,
  ModalBody,
  ModalFooter,
  Form,
  FormGroup,
  FormHelperText,
  HelperText,
  HelperTextItem,
  TextInput,
  Switch,
  Tabs,
  Tab,
  TabTitleText,
  Flex,
  FlexItem,
  Label,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td } from "@patternfly/react-table";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";
import PencilAltIcon from "@patternfly/react-icons/dist/esm/icons/pencil-alt-icon";
import TrashIcon from "@patternfly/react-icons/dist/esm/icons/trash-icon";

import {
  fetchUsers,
  createUser,
  updateUser,
  deleteUser,
  adminSetUserPassword,
  fetchUserBindings,
  createBinding,
  deleteBinding,
  User,
  UserRoleBinding,
  CreateUserRequest,
} from "../../apiClient/usersApi";
import { fetchRoles, Role } from "../../apiClient/rolesApi";
import { hasPermission } from "../../apiClient/permissions";

/* ── Users Tab ── */

export const UsersTab: React.FC = () => {
  const canCreate = hasPermission("user:create");
  const canEdit = hasPermission("user:edit");
  const canDelete = hasPermission("user:delete");
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [editUser, setEditUser] = useState<User | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<User | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const { addToast } = useToast();

  const reload = useCallback(() => {
    setLoading(true);
    setError(null);
    fetchUsers()
      .then(setUsers)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    reload();
  }, [reload]);

  const handleToggleEnabled = async (user: User) => {
    try {
      await updateUser(user.id, { enabled: !user.enabled });
      addToast({
        variant: "success",
        title: user.enabled ? "User disabled" : "User enabled",
        detail: user.username,
      });
      reload();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to toggle user");
    }
  };

  const confirmDelete = async () => {
    if (!deleteTarget) return;
    setDeleteBusy(true);
    setDeleteError(null);
    try {
      await deleteUser(deleteTarget.id);
      addToast({ variant: "success", title: "User deleted", detail: deleteTarget.username });
      setDeleteTarget(null);
      reload();
    } catch (e: unknown) {
      setDeleteError(e instanceof Error ? e.message : "Failed to delete user");
    } finally {
      setDeleteBusy(false);
    }
  };

  if (loading) return <Spinner size="lg" aria-label="Loading" />;

  return (
    <>
      <LiveAlert message={error} isInline style={{ marginBottom: 16 }} />

      {canCreate && (
        <Flex style={{ marginBottom: 16 }}>
          <FlexItem align={{ default: "alignRight" }}>
            <Button
              variant="primary"
              icon={<PlusCircleIcon />}
              onClick={() => setShowCreate(true)}
            >
              Create User
            </Button>
          </FlexItem>
        </Flex>
      )}

      {users.length === 0 ? (
        <BorEmptyState
          isEmptyData
          itemsLabel="users"
          emptyTitle="No users yet"
          emptyBody={canCreate ? "Create a user to grant someone access to the console." : "No users to display."}
          action={
            canCreate ? (
              <Button variant="primary" icon={<PlusCircleIcon />} onClick={() => setShowCreate(true)}>
                Create User
              </Button>
            ) : undefined
          }
        />
      ) : (
        <Table aria-label="Users table" variant="compact">
          <Thead>
            <Tr>
              <Th>Username</Th>
              <Th>Email</Th>
              <Th>Source</Th>
              <Th>Enabled</Th>
              {(canEdit || canDelete) && <Th screenReaderText="Actions" />}
            </Tr>
          </Thead>
          <Tbody>
            {users.map((u) => (
              <Tr key={u.id}>
                <Td>{u.username}</Td>
                <Td>{u.email}</Td>
                <Td>
                  <Label color={u.source === "local" ? "blue" : "purple"}>
                    {u.source}
                  </Label>
                </Td>
                <Td>
                  <Label color={u.enabled ? "green" : "red"}>
                    {u.enabled ? "Yes" : "No"}
                  </Label>
                </Td>
                {(canEdit || canDelete) && (
                  <Td>
                    {canEdit && (
                      <Button
                        variant="plain"
                        aria-label={`Edit user ${u.username}`}
                        onClick={() => setEditUser(u)}
                      >
                        <PencilAltIcon />
                      </Button>
                    )}
                    {canEdit && (
                      <Button
                        variant="plain"
                        aria-label={`${u.enabled ? "Disable" : "Enable"} user ${u.username}`}
                        onClick={() => handleToggleEnabled(u)}
                      >
                        {u.enabled ? "Disable" : "Enable"}
                      </Button>
                    )}
                    {canDelete && (
                      <Button
                        variant="plain"
                        aria-label={`Delete user ${u.username}`}
                        isDanger
                        onClick={() => setDeleteTarget(u)}
                      >
                        <TrashIcon />
                      </Button>
                    )}
                  </Td>
                )}
              </Tr>
            ))}
          </Tbody>
        </Table>
      )}

      <ConfirmModal
        isOpen={deleteTarget !== null}
        title="Delete user?"
        confirmLabel="Delete"
        isDanger
        confirmPhrase={deleteTarget?.username}
        confirmPhraseLabel={
          deleteTarget ? `Type the username "${deleteTarget.username}" to confirm` : undefined
        }
        isBusy={deleteBusy}
        error={deleteError}
        onConfirm={confirmDelete}
        onCancel={() => setDeleteTarget(null)}
      >
        {deleteTarget && (
          <p>
            This permanently deletes the user <strong>{deleteTarget.username}</strong> and their role
            assignments. This cannot be undone.
          </p>
        )}
      </ConfirmModal>

      {showCreate && (
        <CreateUserModal
          onClose={() => setShowCreate(false)}
          onCreated={() => {
            setShowCreate(false);
            addToast({ variant: "success", title: "User created" });
            reload();
          }}
        />
      )}

      {editUser && (
        <EditUserModal
          user={editUser}
          onClose={() => setEditUser(null)}
          onSaved={() => {
            setEditUser(null);
            addToast({ variant: "success", title: "User updated" });
            reload();
          }}
        />
      )}
    </>
  );
};

/* ── Create User Modal ── */

const CreateUserModal: React.FC<{
  onClose: () => void;
  onCreated: () => void;
}> = ({ onClose, onCreated }) => {
  const [username, setUsername] = useState("");
  const [email, setEmail] = useState("");
  const [fullName, setFullName] = useState("");
  const [password, setPassword] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    try {
      const req: CreateUserRequest = {
        username,
        email,
        full_name: fullName,
        password,
      };
      await createUser(req);
      onCreated();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to create user");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      variant={ModalVariant.medium}
      isOpen
      onClose={onClose}
    >
      <ModalHeader title="Create User" />
      <ModalBody>
        <LiveAlert id="err-create-user" message={error} isInline style={{ marginBottom: 16 }} />
        <Form>
          <FormGroup label="Username" isRequired fieldId="cu-username">
            <TextInput
              id="cu-username"
              value={username}
              onChange={(_ev, v) => setUsername(v)}
              isRequired
              aria-invalid={error ? true : undefined}
              aria-describedby={error ? "err-create-user" : undefined}
            />
          </FormGroup>
          <FormGroup label="Email" fieldId="cu-email">
            <TextInput
              id="cu-email"
              type="email"
              value={email}
              onChange={(_ev, v) => setEmail(v)}
            />
          </FormGroup>
          <FormGroup label="Full Name" fieldId="cu-fullname">
            <TextInput
              id="cu-fullname"
              value={fullName}
              onChange={(_ev, v) => setFullName(v)}
            />
          </FormGroup>
          <FormGroup label="Password" isRequired fieldId="cu-password">
            <TextInput
              id="cu-password"
              type="password"
              value={password}
              onChange={(_ev, v) => setPassword(v)}
              isRequired
              aria-invalid={error ? true : undefined}
              aria-describedby={error ? "err-create-user" : undefined}
            />
          </FormGroup>
        </Form>
      </ModalBody>
      <ModalFooter>
        <Button
          key="save"
          variant="primary"
          onClick={handleSave}
          isDisabled={saving || !username || !password}
          isLoading={saving}
        >
          Create
        </Button>
        <Button key="cancel" variant="link" onClick={onClose}>
          Cancel
        </Button>
      </ModalFooter>
    </Modal>
  );
};

/* ── Edit User Modal (with Profile + Role Assignments tabs) ── */

const EditUserModal: React.FC<{
  user: User;
  onClose: () => void;
  onSaved: () => void;
}> = ({ user, onClose, onSaved }) => {
  const [activeTab, setActiveTab] = useState("profile");

  return (
    <Modal
      variant={ModalVariant.large}
      isOpen
      onClose={onClose}
    >
      <ModalHeader title={`Edit User: ${user.username}`} />
      <ModalBody>
        <Tabs
          activeKey={activeTab}
          onSelect={(_ev, k) => setActiveTab(k as string)}
        >
          <Tab
            eventKey="profile"
            title={<TabTitleText>Profile</TabTitleText>}
          >
            <div style={{ padding: "16px 0" }}>
              <ProfileTab user={user} onSaved={onSaved} />
            </div>
          </Tab>
          <Tab
            eventKey="roles"
            title={<TabTitleText>Role Assignments</TabTitleText>}
          >
            <div style={{ padding: "16px 0" }}>
              <RoleAssignmentsTab userId={user.id} />
            </div>
          </Tab>
          {user.source === "local" && (
            <Tab
              eventKey="password"
              title={<TabTitleText>Password</TabTitleText>}
            >
              <div style={{ padding: "16px 0" }}>
                <AdminPasswordTab userId={user.id} />
              </div>
            </Tab>
          )}
        </Tabs>
      </ModalBody>
    </Modal>
  );
};

/* ── Profile Tab ── */

const ProfileTab: React.FC<{
  user: User;
  onSaved: () => void;
}> = ({ user, onSaved }) => {
  const [email, setEmail] = useState(user.email);
  const [fullName, setFullName] = useState(user.full_name);
  const [enabled, setEnabled] = useState(user.enabled);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    try {
      await updateUser(user.id, { email, full_name: fullName, enabled });
      onSaved();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to update user");
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <LiveAlert message={error} isInline style={{ marginBottom: 16 }} />
      <Form>
        <FormGroup label="Email" fieldId="eu-email">
          <TextInput
            id="eu-email"
            type="email"
            value={email}
            onChange={(_ev, v) => setEmail(v)}
          />
        </FormGroup>
        <FormGroup label="Full Name" fieldId="eu-fullname">
          <TextInput
            id="eu-fullname"
            value={fullName}
            onChange={(_ev, v) => setFullName(v)}
          />
        </FormGroup>
        <FormGroup label="Enabled" fieldId="eu-enabled">
          <Switch
            id="eu-enabled"
            isChecked={enabled}
            onChange={(_ev, v) => setEnabled(v)}
          />
        </FormGroup>
        <Button
          variant="primary"
          onClick={handleSave}
          isDisabled={saving}
          isLoading={saving}
        >
          Save
        </Button>
      </Form>
    </>
  );
};

/* ── Role Assignments Tab ── */

const RoleAssignmentsTab: React.FC<{ userId: string }> = ({ userId }) => {
  const [bindings, setBindings] = useState<UserRoleBinding[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showAdd, setShowAdd] = useState(false);

  // Add role form state
  const [newRoleId, setNewRoleId] = useState("");
  const [newScopeType, setNewScopeType] = useState("global");
  const [newScopeId, setNewScopeId] = useState("");
  const [addSaving, setAddSaving] = useState(false);

  const reload = useCallback(() => {
    setLoading(true);
    setError(null);
    Promise.all([fetchUserBindings(userId), fetchRoles()])
      .then(([b, r]) => {
        setBindings(b);
        setRoles(r);
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, [userId]);

  useEffect(() => {
    reload();
  }, [reload]);

  const roleName = (roleId: string) => {
    const r = roles.find((role) => role.id === roleId);
    return r ? r.name : roleId;
  };

  const handleRemove = async (bindingId: string) => {
    try {
      await deleteBinding(bindingId);
      reload();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to remove binding");
    }
  };

  const handleAdd = async () => {
    if (!newRoleId) return;
    setAddSaving(true);
    setError(null);
    try {
      await createBinding({
        user_id: userId,
        role_id: newRoleId,
        scope_type: newScopeType,
        scope_id: newScopeType !== "global" ? newScopeId : undefined,
      });
      setShowAdd(false);
      setNewRoleId("");
      setNewScopeType("global");
      setNewScopeId("");
      reload();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to add binding");
    } finally {
      setAddSaving(false);
    }
  };

  if (loading) return <Spinner size="lg" aria-label="Loading" />;

  return (
    <>
      <LiveAlert message={error} isInline style={{ marginBottom: 16 }} />

      <Flex style={{ marginBottom: 16 }}>
        <FlexItem align={{ default: "alignRight" }}>
          <Button
            variant="primary"
            icon={<PlusCircleIcon />}
            onClick={() => setShowAdd(true)}
          >
            Add Role
          </Button>
        </FlexItem>
      </Flex>

      <Table aria-label="Role assignments" variant="compact">
        <Thead>
          <Tr>
            <Th>Role</Th>
            <Th>Scope Type</Th>
            <Th>Scope Target</Th>
            <Th>Actions</Th>
          </Tr>
        </Thead>
        <Tbody>
          {bindings.map((b) => (
            <Tr key={b.id}>
              <Td>{roleName(b.role_id)}</Td>
              <Td>
                <Label>{b.scope_type}</Label>
              </Td>
              <Td>{b.scope_id || "—"}</Td>
              <Td>
                <Button
                  variant="plain"
                  isDanger
                  aria-label="Remove"
                  onClick={() => handleRemove(b.id)}
                >
                  <TrashIcon />
                </Button>
              </Td>
            </Tr>
          ))}
          {bindings.length === 0 && (
            <Tr>
              <Td colSpan={4}>No role assignments.</Td>
            </Tr>
          )}
        </Tbody>
      </Table>

      {showAdd && (
        <Modal
          variant={ModalVariant.small}
          isOpen
          onClose={() => setShowAdd(false)}
        >
          <ModalHeader title="Add Role Assignment" />
          <ModalBody>
            <Form>
              <FormGroup label="Role" isRequired fieldId="ar-role">
                <select
                  id="ar-role"
                  className="pf-v6-c-form-control"
                  value={newRoleId}
                  onChange={(e) => setNewRoleId(e.target.value)}
                >
                  <option value="">Select a role...</option>
                  {roles.map((r) => (
                    <option key={r.id} value={r.id}>
                      {r.name}
                    </option>
                  ))}
                </select>
              </FormGroup>
              <FormGroup label="Scope Type" isRequired fieldId="ar-scope">
                <select
                  id="ar-scope"
                  className="pf-v6-c-form-control"
                  value={newScopeType}
                  onChange={(e) => setNewScopeType(e.target.value)}
                >
                  <option value="global">Global</option>
                  <option value="organization">Organization</option>
                  <option value="group">User Group</option>
                </select>
              </FormGroup>
              {newScopeType !== "global" && (
                <FormGroup label="Scope Target" fieldId="ar-scope-id">
                  <TextInput
                    id="ar-scope-id"
                    value={newScopeId}
                    onChange={(_ev, v) => setNewScopeId(v)}
                    placeholder="Enter scope ID"
                  />
                </FormGroup>
              )}
            </Form>
          </ModalBody>
          <ModalFooter>
            <Button
              key="add"
              variant="primary"
              onClick={handleAdd}
              isDisabled={addSaving || !newRoleId}
              isLoading={addSaving}
            >
              Add
            </Button>
            <Button key="cancel" variant="link" onClick={() => setShowAdd(false)}>
              Cancel
            </Button>
          </ModalFooter>
        </Modal>
      )}
    </>
  );
};

/* ── Admin Password Tab ── */

const AdminPasswordTab: React.FC<{ userId: string }> = ({ userId }) => {
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const mismatch = confirm !== "" && next !== confirm;
  const canSubmit = next !== "" && confirm !== "" && !mismatch;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    setSaving(true);
    setError(null);
    setSuccess(null);
    try {
      await adminSetUserPassword(userId, next);
      setSuccess("Password updated successfully.");
      setNext("");
      setConfirm("");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to set password");
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <LiveAlert message={error} isInline style={{ marginBottom: 16 }} />
      <LiveAlert message={success} isInline variant="success" style={{ marginBottom: 16 }} />
      <Form onSubmit={handleSubmit} style={{ maxWidth: 480 }}>
        <FormGroup label="New password" isRequired fieldId="ap-new">
          <TextInput
            id="ap-new"
            type="password"
            value={next}
            onChange={(_ev, v) => setNext(v)}
            autoComplete="new-password"
            aria-required
          />
        </FormGroup>
        <FormGroup label="Confirm new password" isRequired fieldId="ap-confirm">
          <TextInput
            id="ap-confirm"
            type="password"
            value={confirm}
            onChange={(_ev, v) => setConfirm(v)}
            autoComplete="new-password"
            validated={mismatch ? "error" : "default"}
            aria-required
            aria-invalid={mismatch || undefined}
            aria-describedby={mismatch ? "ap-confirm-error" : undefined}
          />
          {mismatch && (
            <FormHelperText>
              <HelperText>
                <HelperTextItem id="ap-confirm-error" variant="error">
                  Passwords do not match
                </HelperTextItem>
              </HelperText>
            </FormHelperText>
          )}
        </FormGroup>
        <Button
          type="submit"
          variant="primary"
          isDisabled={!canSubmit || saving}
          isLoading={saving}
        >
          Set password
        </Button>
      </Form>
    </>
  );
};
