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
  Checkbox,
  Title,
  Flex,
  FlexItem,
} from "@patternfly/react-core";
import { Table, Thead, Tr, Th, Tbody, Td } from "@patternfly/react-table";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";
import PencilAltIcon from "@patternfly/react-icons/dist/esm/icons/pencil-alt-icon";
import TrashIcon from "@patternfly/react-icons/dist/esm/icons/trash-icon";

import {
  fetchRoles,
  createRole,
  updateRole,
  deleteRole,
  fetchAllPermissions,
  fetchRolePermissions,
  setRolePermissions,
  Role,
  Permission,
} from "../../apiClient/rolesApi";

/* ── Roles Tab ── */

export const RolesTab: React.FC = () => {
  const [roles, setRoles] = useState<Role[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showCreate, setShowCreate] = useState(false);
  const [editRole, setEditRole] = useState<Role | null>(null);

  const reload = useCallback(() => {
    setLoading(true);
    setError(null);
    fetchRoles()
      .then(setRoles)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    reload();
  }, [reload]);

  const handleDelete = async (role: Role) => {
    if (!confirm(`Delete role "${role.name}"?`)) return;
    try {
      await deleteRole(role.id);
      reload();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to delete role");
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
          <Button
            variant="primary"
            icon={<PlusCircleIcon />}
            onClick={() => setShowCreate(true)}
          >
            Create Role
          </Button>
        </FlexItem>
      </Flex>

      <Table aria-label="Roles table" variant="compact">
        <Thead>
          <Tr>
            <Th>Role Name</Th>
            <Th>Description</Th>
            <Th># Permissions</Th>
            <Th>Actions</Th>
          </Tr>
        </Thead>
        <Tbody>
          {roles.map((role) => (
            <Tr key={role.id}>
              <Td>{role.name}</Td>
              <Td>{role.description}</Td>
              <Td>{role.permission_count}</Td>
              <Td>
                <Button
                  variant="plain"
                  aria-label="Edit"
                  onClick={() => setEditRole(role)}
                >
                  <PencilAltIcon />
                </Button>
                <Button
                  variant="plain"
                  aria-label="Delete"
                  isDanger
                  onClick={() => handleDelete(role)}
                >
                  <TrashIcon />
                </Button>
              </Td>
            </Tr>
          ))}
          {roles.length === 0 && (
            <Tr>
              <Td colSpan={4}>No roles found.</Td>
            </Tr>
          )}
        </Tbody>
      </Table>

      {showCreate && (
        <CreateRoleModal
          onClose={() => setShowCreate(false)}
          onCreated={() => {
            setShowCreate(false);
            reload();
          }}
        />
      )}

      {editRole && (
        <EditRoleModal
          role={editRole}
          onClose={() => setEditRole(null)}
          onSaved={() => {
            setEditRole(null);
            reload();
          }}
        />
      )}
    </>
  );
};

/* ── Create Role Modal ── */

const CreateRoleModal: React.FC<{
  onClose: () => void;
  onCreated: () => void;
}> = ({ onClose, onCreated }) => {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    try {
      await createRole({ name, description });
      onCreated();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to create role");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      variant={ModalVariant.medium}
      title="Create Role"
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
        <FormGroup label="Role Name" isRequired fieldId="cr-name">
          <TextInput
            id="cr-name"
            value={name}
            onChange={(_ev, v) => setName(v)}
            isRequired
          />
        </FormGroup>
        <FormGroup label="Description" fieldId="cr-desc">
          <TextArea
            id="cr-desc"
            value={description}
            onChange={(_ev, v) => setDescription(v)}
          />
        </FormGroup>
      </Form>
    </Modal>
  );
};

/* ── Edit Role Modal (details + permission matrix) ── */

const EditRoleModal: React.FC<{
  role: Role;
  onClose: () => void;
  onSaved: () => void;
}> = ({ role, onClose, onSaved }) => {
  const [name, setName] = useState(role.name);
  const [description, setDescription] = useState(role.description);

  const [allPermissions, setAllPermissions] = useState<Permission[]>([]);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    Promise.all([fetchAllPermissions(), fetchRolePermissions(role.id)])
      .then(([allPerms, rolePerms]) => {
        setAllPermissions(allPerms);
        setSelectedIds(new Set(rolePerms.map((p) => p.id)));
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false));
  }, [role.id]);

  const handleToggle = (permId: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(permId)) {
        next.delete(permId);
      } else {
        next.add(permId);
      }
      return next;
    });
  };

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    try {
      await updateRole(role.id, { name, description });
      await setRolePermissions(role.id, Array.from(selectedIds));
      onSaved();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "Failed to save role");
    } finally {
      setSaving(false);
    }
  };

  // Group permissions by resource for the matrix display
  const grouped: Record<string, Permission[]> = {};
  for (const p of allPermissions) {
    if (!grouped[p.resource]) grouped[p.resource] = [];
    grouped[p.resource].push(p);
  }

  return (
    <Modal
      variant={ModalVariant.large}
      title={`Edit Role: ${role.name}`}
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
          Save
        </Button>,
        <Button key="cancel" variant="link" onClick={onClose}>
          Cancel
        </Button>,
      ]}
    >
      {error && <Alert variant="danger" title={error} isInline style={{ marginBottom: 16 }} />}
      {loading ? (
        <Spinner size="lg" />
      ) : (
        <>
          <Form style={{ marginBottom: 24 }}>
            <FormGroup label="Role Name" isRequired fieldId="er-name">
              <TextInput
                id="er-name"
                value={name}
                onChange={(_ev, v) => setName(v)}
                isRequired
              />
            </FormGroup>
            <FormGroup label="Description" fieldId="er-desc">
              <TextArea
                id="er-desc"
                value={description}
                onChange={(_ev, v) => setDescription(v)}
              />
            </FormGroup>
          </Form>

          <Title headingLevel="h3" style={{ marginBottom: 12 }}>
            Permission Matrix
          </Title>

          {Object.entries(grouped)
            .sort(([a], [b]) => a.localeCompare(b))
            .map(([resource, perms]) => (
              <div key={resource} style={{ marginBottom: 16 }}>
                <Title headingLevel="h4" style={{ textTransform: "capitalize", marginBottom: 8 }}>
                  {resource}
                </Title>
                <div style={{ display: "flex", flexWrap: "wrap", gap: "8px 24px" }}>
                  {perms
                    .sort((a, b) => a.action.localeCompare(b.action))
                    .map((p) => (
                      <Checkbox
                        key={p.id}
                        id={`perm-${p.id}`}
                        label={p.action}
                        isChecked={selectedIds.has(p.id)}
                        onChange={() => handleToggle(p.id)}
                      />
                    ))}
                </div>
              </div>
            ))}
        </>
      )}
    </Modal>
  );
};
