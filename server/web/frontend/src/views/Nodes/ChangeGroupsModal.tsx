// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * ChangeGroupsModal - sets the node group membership of one or several nodes
 * in one dialog. Every group is a checkbox, pre-checked from the current
 * membership. For several nodes a group that only some of them are in starts
 * in the mixed state, and only the groups the admin changed are applied.
 */

import React, { useEffect, useMemo, useState } from "react";
import {
  Button,
  Checkbox,
  Form,
  FormGroup,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  ModalVariant,
  SearchInput,
} from "@patternfly/react-core";

import { LiveAlert } from "../../components/LiveAlert";
import { Node, removeNodeFromGroup } from "../../apiClient/nodesApi";
import { addNodesToGroup, NodeGroup } from "../../apiClient/nodeGroupsApi";
import { hasPermission } from "../../apiClient/permissions";

/** true = all targets are members, false = none, null = some (mixed). */
type Membership = boolean | null;

/** Show the filter box only when the list is long enough to need it. */
const FILTER_THRESHOLD = 8;

interface ChangeGroupsModalProps {
  isOpen: boolean;
  /** The nodes whose membership is edited, with their current groups. */
  targets: Node[];
  /** All node groups the user can see. */
  groups: NodeGroup[];
  onClose: () => void;
  /** Called after at least one change was applied, so the list can reload. */
  onChanged: () => void;
  /** Called when every change was applied; the dialog should close. */
  onDone: () => void;
}

const isMember = (node: Node, groupId: string): boolean =>
  node.node_group_ids?.includes(groupId) ?? false;

const memberCount = (targets: Node[], groupId: string): number =>
  targets.filter((n) => isMember(n, groupId)).length;

const currentMembership = (targets: Node[], groupId: string): Membership => {
  const count = memberCount(targets, groupId);
  if (count === 0) return false;
  return count === targets.length ? true : null;
};

export const ChangeGroupsModal: React.FC<ChangeGroupsModalProps> = ({
  isOpen,
  targets,
  groups,
  onClose,
  onChanged,
  onDone,
}) => {
  const canAdd = hasPermission("node:create");
  const canRemove = hasPermission("node:delete");

  // Only the groups the admin touched; everything else follows `targets`.
  const [choices, setChoices] = useState<Record<string, Membership>>({});
  const [filter, setFilter] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (isOpen) {
      setChoices({});
      setFilter("");
      setSaving(false);
      setError(null);
    }
  }, [isOpen]);

  const rows = useMemo(
    () =>
      groups.map((g) => {
        const current = currentMembership(targets, g.id);
        const chosen = g.id in choices ? choices[g.id] : current;
        return { group: g, current, chosen, members: memberCount(targets, g.id) };
      }),
    [groups, targets, choices],
  );

  const toAdd = rows.filter((r) => r.chosen === true && r.current !== true);
  const toRemove = rows.filter((r) => r.chosen === false && r.current !== false);
  const hasChanges = toAdd.length > 0 || toRemove.length > 0;

  // A mixed group cycles mixed -> all -> none -> mixed; the others toggle.
  // States the user has no permission to reach are skipped.
  const nextChoice = (current: Membership, chosen: Membership): Membership => {
    const order: Membership[] = current === null ? [null, true, false] : [current, !current];
    const allowed = order.filter(
      (state) =>
        state === current ||
        (state === true && canAdd) ||
        (state === false && canRemove),
    );
    return allowed[(allowed.indexOf(chosen) + 1) % allowed.length];
  };

  const isLocked = (current: Membership): boolean =>
    (current === false && !canAdd) ||
    (current === true && !canRemove) ||
    (current === null && !canAdd && !canRemove);

  const handleSave = async () => {
    setSaving(true);
    setError(null);
    const ops: Promise<unknown>[] = [];
    toAdd.forEach(({ group }) => {
      const ids = targets.filter((n) => !isMember(n, group.id)).map((n) => n.id);
      if (ids.length > 0) ops.push(addNodesToGroup(group.id, ids));
    });
    toRemove.forEach(({ group }) => {
      targets
        .filter((n) => isMember(n, group.id))
        .forEach((n) => ops.push(removeNodeFromGroup(n.id, group.id)));
    });
    const results = await Promise.allSettled(ops);
    setSaving(false);

    const failed = results.filter((r): r is PromiseRejectedResult => r.status === "rejected");
    if (failed.length < results.length) onChanged();
    if (failed.length > 0) {
      const reason = failed[0].reason;
      setError(
        `${failed.length} of ${results.length} changes failed: ${
          reason instanceof Error ? reason.message : "request failed"
        }`,
      );
      return;
    }
    onDone();
  };

  const needle = filter.trim().toLowerCase();
  const visible = needle
    ? rows.filter((r) => r.group.name.toLowerCase().includes(needle))
    : rows;

  const single = targets.length === 1 ? targets[0] : null;
  const names = (list: typeof rows) => list.map((r) => r.group.name).join(", ");

  return (
    <Modal
      variant={ModalVariant.small}
      isOpen={isOpen}
      onClose={onClose}
      aria-labelledby="change-groups-title"
    >
      <ModalHeader
        title={single ? `Node groups of ${single.name}` : `Node groups of ${targets.length} nodes`}
        labelId="change-groups-title"
        description={
          single
            ? "Tick every group the node should be in."
            : "Tick a group to put all selected nodes in it, clear it to remove them all. Groups left in the mixed state are not changed."
        }
      />
      <ModalBody>
        <Form onSubmit={(e) => e.preventDefault()}>
          <LiveAlert id="err-change-groups" message={error} variant="danger" isInline />
          {groups.length === 0 ? (
            <p>There are no node groups yet. Create one on the Node Groups page.</p>
          ) : (
            <>
              {groups.length > FILTER_THRESHOLD && (
                <SearchInput
                  aria-label="Filter node groups"
                  placeholder="Filter groups..."
                  value={filter}
                  onChange={(_ev, val) => setFilter(val)}
                  onClear={() => setFilter("")}
                />
              )}
              <FormGroup role="group" label="Node groups" fieldId="change-groups-list">
                <div
                  id="change-groups-list"
                  style={{ maxHeight: "40vh", overflowY: "auto", padding: "0.25rem" }}
                >
                  {visible.length === 0 && <p className="bor-text-secondary">No groups match.</p>}
                  {visible.map(({ group, current, chosen, members }) => (
                    <Checkbox
                      key={group.id}
                      id={`change-group-${group.id}`}
                      label={group.name}
                      description={
                        current === null
                          ? `${members} of ${targets.length} selected nodes are members`
                          : undefined
                      }
                      isChecked={chosen}
                      isDisabled={saving || isLocked(current)}
                      onChange={() =>
                        setChoices((prev) => ({
                          ...prev,
                          [group.id]: nextChoice(current, chosen),
                        }))
                      }
                    />
                  ))}
                </div>
              </FormGroup>
              <p aria-live="polite" className="bor-text-secondary">
                {!hasChanges && "No changes."}
                {toAdd.length > 0 && `Add to: ${names(toAdd)}. `}
                {toRemove.length > 0 && `Remove from: ${names(toRemove)}.`}
              </p>
              <LiveAlert
                message={
                  toRemove.length > 0
                    ? "Removing a node from a group stops the policies bound to that group on it."
                    : null
                }
                variant="warning"
                isInline
                isPlain
              />
            </>
          )}
        </Form>
      </ModalBody>
      <ModalFooter>
        <Button
          variant="primary"
          onClick={handleSave}
          isLoading={saving}
          isDisabled={saving || !hasChanges}
        >
          Save
        </Button>
        <Button variant="link" onClick={onClose} isDisabled={saving}>
          Cancel
        </Button>
      </ModalFooter>
    </Modal>
  );
};
