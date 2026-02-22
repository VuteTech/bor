// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useState, useEffect, useCallback } from "react";
import {
  Button,
  Alert,
  Spinner,
  Modal,
  ModalVariant,
  Form,
  FormGroup,
  TextInput,
  TextArea,
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

import { hasPermission } from "../../apiClient/permissions";
import {
  fetchUserGroups,
  createUserGroup,
  updateUserGroup,
  deleteUserGroup,
  fetchGroupMembers,
  addGroupMember,
  removeGroupMember,
  fetchGroupRoleBindings,
  addGroupRoleBinding,
  removeGroupRoleBinding,
  UserGroup,
  UserGroupMember,
  UserGroupRoleBinding,
} from "../../apiClient/userGroupsApi";
import { fetchUsers, User } from "../../apiClient/usersApi";
import { fetchRoles, Role } from "../../apiClient/rolesApi";

export const UserGroupsTab: React.FC = () => {
  const [groups, setGroups] = useState<UserGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [editGroup, setEditGroup] = useState<UserGroup | null>(null);

  const canCreate = hasPermission("user_group:create");
  const canEdit = hasPermission("user_group:edit");
  const canDelete = hasPermission("user_group:delete");

  const reload = useCallback(() => {
    setLoading(true);
    setError(null);
    fetchUserGroups()
      .then(setGroups)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    reload();
  }, [reload]);

  const handleDelete = async (group: UserGroup) => {
    if (!confirm(`Delete user group "${group.name}"?`)) return;
    try {
      await deleteUserGroup(group.id);
      reload();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to delete user group");
    }
  };

  const formatDate = (dateStr: string) => {
    try {
      return new Date(dateStr).toLocaleDateString();
    } catch {
      return dateStr;
    }
  };

  if (loading) return <Spinner size="lg" />;

  return (
    <>
      {error && (
        <Alert variant="danger" title={error} isInline style={{ marginBottom: 16 }} />
      )}

      <Flex style={{ marginBottom: 16 }}>
        <FlexItem align={{ default: "alignRight" }}>
          {canCreate && (
            <Button
              variant="primary"
              icon={<PlusCircleIcon />}
              onClick={() => setShowCreate(true)}
            >
              Create User Group
            </Button>
          )}
        </FlexItem>
      </Flex>

      <Table aria-label="User groups table" variant="compact">
        <Thead>
          <Tr>
            <Th>Name</Th>
            <Th>Description</Th>
            <Th>Created At</Th>
            {(canEdit || canDelete) && <Th>Actions</Th>}
          </Tr>
        </Thead>
        <Tbody>
          {groups.map((g) => (
            <Tr key={g.id}>
              <Td>{g.name}</Td>
              <Td>{g.description}</Td>
              <Td>{formatDate(g.created_at)}</Td>
              {(canEdit || canDelete) && (
                <Td>
                  {canEdit && (
                    <Button
                      variant="plain"
                      aria-label="Edit"
                      onClick={() => setEditGroup(g)}
                    >
                      <PencilAltIcon />
                    </Button>
                  )}
                  {canDelete && (
                    <Button
                      variant="plain"
                      aria-label="Delete"
                      isDanger
                      onClick={() => handleDelete(g)}
                    >
                      <TrashIcon />
                    </Button>
                  )}
                </Td>
              )}
            </Tr>
          ))}
          {groups.length === 0 && (
            <Tr>
              <Td colSpan={(canEdit || canDelete) ? 4 : 3}>No user groups found.</Td>
            </Tr>
          )}
        </Tbody>
      </Table>

      {showCreate && (
        <CreateGroupModal
          onClose={() => setShowCreate(false)}
          onSaved={() => {
            setShowCreate(false);
            reload();
          }}
        />
      )}

      {editGroup && (
        <EditGroupModal
          group={editGroup}
          onClose={() => setEditGroup(null)}
          onSaved={() => {
            setEditGroup(null);
            reload();
          }}
        />
      )}
    </>
  );
};

/* ── Create User Group Modal ── */

const CreateGroupModal: React.FC<{
  onClose: () => void;
  onSaved: () => void;
}> = ({ onClose, onSaved }) => {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    try {
      await createUserGroup({ name, description });
      onSaved();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to create user group");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      variant={ModalVariant.medium}
      title="Create User Group"
      isOpen
      onClose={onClose}
      actions={[
        <Button
          key="save"
          variant="primary"
          onClick={handleSave}
          isDisabled={saving || !name}
          isLoading={saving}
        >
          Create
        </Button>,
        <Button key="cancel" variant="link" onClick={onClose}>
          Cancel
        </Button>,
      ]}
    >
      {error && <Alert variant="danger" title={error} isInline style={{ marginBottom: 16 }} />}
      <Form>
        <FormGroup label="Name" isRequired fieldId="cg-name">
          <TextInput
            id="cg-name"
            value={name}
            onChange={(_ev, v) => setName(v)}
            isRequired
          />
        </FormGroup>
        <FormGroup label="Description" fieldId="cg-desc">
          <TextArea
            id="cg-desc"
            value={description}
            onChange={(_ev, v) => setDescription(v)}
          />
        </FormGroup>
      </Form>
    </Modal>
  );
};

/* ── Edit User Group Modal (with Details, Members, Role Assignments tabs) ── */

const EditGroupModal: React.FC<{
  group: UserGroup;
  onClose: () => void;
  onSaved: () => void;
}> = ({ group, onClose, onSaved }) => {
  const [activeTab, setActiveTab] = useState("details");

  return (
    <Modal
      variant={ModalVariant.large}
      title={`Edit User Group: ${group.name}`}
      isOpen
      onClose={onClose}
    >
      <Tabs
        activeKey={activeTab}
        onSelect={(_ev, k) => setActiveTab(k as string)}
      >
        <Tab eventKey="details" title={<TabTitleText>Details</TabTitleText>}>
          <div style={{ padding: "16px 0" }}>
            <DetailsTab group={group} onSaved={onSaved} />
          </div>
        </Tab>
        <Tab eventKey="members" title={<TabTitleText>Members</TabTitleText>}>
          <div style={{ padding: "16px 0" }}>
            <MembersTab groupId={group.id} />
          </div>
        </Tab>
        <Tab eventKey="roles" title={<TabTitleText>Role Assignments</TabTitleText>}>
          <div style={{ padding: "16px 0" }}>
            <GroupRoleAssignmentsTab groupId={group.id} />
          </div>
        </Tab>
      </Tabs>
    </Modal>
  );
};

/* ── Details Tab ── */

const DetailsTab: React.FC<{
  group: UserGroup;
  onSaved: () => void;
}> = ({ group, onSaved }) => {
  const [name, setName] = useState(group.name);
  const [description, setDescription] = useState(group.description);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    try {
      await updateUserGroup(group.id, { name, description });
      onSaved();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to update user group");
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      {error && <Alert variant="danger" title={error} isInline style={{ marginBottom: 16 }} />}
      <Form>
        <FormGroup label="Name" isRequired fieldId="eg-name">
          <TextInput
            id="eg-name"
            value={name}
            onChange={(_ev, v) => setName(v)}
            isRequired
          />
        </FormGroup>
        <FormGroup label="Description" fieldId="eg-desc">
          <TextArea
            id="eg-desc"
            value={description}
            onChange={(_ev, v) => setDescription(v)}
          />
        </FormGroup>
        <Button
          variant="primary"
          onClick={handleSave}
          isDisabled={saving || !name}
          isLoading={saving}
        >
          Save
        </Button>
      </Form>
    </>
  );
};

/* ── Members Tab ── */

const MembersTab: React.FC<{ groupId: string }> = ({ groupId }) => {
  const [members, setMembers] = useState<UserGroupMember[]>([]);
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showAdd, setShowAdd] = useState(false);
  const [selectedUserId, setSelectedUserId] = useState("");
  const [addSaving, setAddSaving] = useState(false);

  const reload = useCallback(() => {
    setLoading(true);
    setError(null);
    Promise.all([fetchGroupMembers(groupId), fetchUsers()])
      .then(([m, u]) => {
        setMembers(m);
        setUsers(u);
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, [groupId]);

  useEffect(() => {
    reload();
  }, [reload]);

  const userName = (userId: string) => {
    const u = users.find((user) => user.id === userId);
    if (!u) return userId;
    const detail = u.email || u.full_name;
    return detail ? `${u.username} (${detail})` : u.username;
  };

  const handleRemove = async (memberId: string) => {
    try {
      await removeGroupMember(groupId, memberId);
      reload();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to remove member");
    }
  };

  const handleAdd = async () => {
    if (!selectedUserId) return;
    setAddSaving(true);
    setError(null);
    try {
      await addGroupMember(groupId, selectedUserId);
      setShowAdd(false);
      setSelectedUserId("");
      reload();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to add member");
    } finally {
      setAddSaving(false);
    }
  };

  // Users not already in the group
  const memberUserIds = new Set(members.map((m) => m.user_id));
  const availableUsers = users.filter((u) => !memberUserIds.has(u.id));

  if (loading) return <Spinner size="lg" />;

  return (
    <>
      {error && <Alert variant="danger" title={error} isInline style={{ marginBottom: 16 }} />}

      <Flex style={{ marginBottom: 16 }}>
        <FlexItem align={{ default: "alignRight" }}>
          <Button
            variant="primary"
            icon={<PlusCircleIcon />}
            onClick={() => setShowAdd(true)}
          >
            Add Member
          </Button>
        </FlexItem>
      </Flex>

      <Table aria-label="Group members" variant="compact">
        <Thead>
          <Tr>
            <Th>User</Th>
            <Th>Added At</Th>
            <Th>Actions</Th>
          </Tr>
        </Thead>
        <Tbody>
          {members.map((m) => (
            <Tr key={m.id}>
              <Td>{userName(m.user_id)}</Td>
              <Td>{new Date(m.created_at).toLocaleDateString()}</Td>
              <Td>
                <Button
                  variant="plain"
                  isDanger
                  aria-label="Remove"
                  onClick={() => handleRemove(m.id)}
                >
                  <TrashIcon />
                </Button>
              </Td>
            </Tr>
          ))}
          {members.length === 0 && (
            <Tr>
              <Td colSpan={3}>No members in this group.</Td>
            </Tr>
          )}
        </Tbody>
      </Table>

      {showAdd && (
        <Modal
          variant={ModalVariant.small}
          title="Add Member"
          isOpen
          onClose={() => setShowAdd(false)}
          actions={[
            <Button
              key="add"
              variant="primary"
              onClick={handleAdd}
              isDisabled={addSaving || !selectedUserId}
              isLoading={addSaving}
            >
              Add
            </Button>,
            <Button key="cancel" variant="link" onClick={() => setShowAdd(false)}>
              Cancel
            </Button>,
          ]}
        >
          <Form>
            <FormGroup label="User" isRequired fieldId="am-user">
              <select
                id="am-user"
                className="pf-v5-c-form-control"
                value={selectedUserId}
                onChange={(e) => setSelectedUserId(e.target.value)}
              >
                <option value="">Select a user...</option>
                {availableUsers.map((u) => {
                  const detail = u.email || u.full_name;
                  return (
                    <option key={u.id} value={u.id}>
                      {detail ? `${u.username} (${detail})` : u.username}
                    </option>
                  );
                })}
              </select>
            </FormGroup>
          </Form>
        </Modal>
      )}
    </>
  );
};

/* ── Group Role Assignments Tab ── */

const GroupRoleAssignmentsTab: React.FC<{ groupId: string }> = ({ groupId }) => {
  const [bindings, setBindings] = useState<UserGroupRoleBinding[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showAdd, setShowAdd] = useState(false);

  const [newRoleId, setNewRoleId] = useState("");
  const [newScopeType, setNewScopeType] = useState("global");
  const [newScopeId, setNewScopeId] = useState("");
  const [addSaving, setAddSaving] = useState(false);

  const reload = useCallback(() => {
    setLoading(true);
    setError(null);
    Promise.all([fetchGroupRoleBindings(groupId), fetchRoles()])
      .then(([b, r]) => {
        setBindings(b);
        setRoles(r);
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, [groupId]);

  useEffect(() => {
    reload();
  }, [reload]);

  const roleName = (roleId: string) => {
    const r = roles.find((role) => role.id === roleId);
    return r ? r.name : roleId;
  };

  const handleRemove = async (bindingId: string) => {
    try {
      await removeGroupRoleBinding(groupId, bindingId);
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
      await addGroupRoleBinding(
        groupId,
        newRoleId,
        newScopeType,
        newScopeType !== "global" ? newScopeId : undefined
      );
      setShowAdd(false);
      setNewRoleId("");
      setNewScopeType("global");
      setNewScopeId("");
      reload();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to add role binding");
    } finally {
      setAddSaving(false);
    }
  };

  if (loading) return <Spinner size="lg" />;

  return (
    <>
      {error && <Alert variant="danger" title={error} isInline style={{ marginBottom: 16 }} />}

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

      <Table aria-label="Group role assignments" variant="compact">
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
          title="Add Role Assignment"
          isOpen
          onClose={() => setShowAdd(false)}
          actions={[
            <Button
              key="add"
              variant="primary"
              onClick={handleAdd}
              isDisabled={addSaving || !newRoleId}
              isLoading={addSaving}
            >
              Add
            </Button>,
            <Button key="cancel" variant="link" onClick={() => setShowAdd(false)}>
              Cancel
            </Button>,
          ]}
        >
          <Form>
            <FormGroup label="Role" isRequired fieldId="gr-role">
              <select
                id="gr-role"
                className="pf-v5-c-form-control"
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
            <FormGroup label="Scope Type" isRequired fieldId="gr-scope">
              <select
                id="gr-scope"
                className="pf-v5-c-form-control"
                value={newScopeType}
                onChange={(e) => setNewScopeType(e.target.value)}
              >
                <option value="global">Global</option>
                <option value="organization">Organization</option>
                <option value="group">User Group</option>
              </select>
            </FormGroup>
            {newScopeType !== "global" && (
              <FormGroup label="Scope Target" fieldId="gr-scope-id">
                <TextInput
                  id="gr-scope-id"
                  value={newScopeId}
                  onChange={(_ev, v) => setNewScopeId(v)}
                  placeholder="Enter scope ID"
                />
              </FormGroup>
            )}
          </Form>
        </Modal>
      )}
    </>
  );
};
