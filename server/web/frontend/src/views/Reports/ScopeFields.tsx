// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

/**
 * ScopeFields: the report scope (all nodes or node groups, and policy types)
 * shared by the generate dialog and the schedule editor.
 */

import React, { useState } from "react";
import {
  FormGroup,
  FormHelperText,
  HelperText,
  HelperTextItem,
  MenuToggle,
  MenuToggleElement,
  Radio,
  Select,
  SelectList,
  SelectOption,
} from "@patternfly/react-core";

import { NodeGroup } from "../../apiClient/nodeGroupsApi";
import { POLICY_TYPE_LIST } from "../../policyTypes/registry";

interface ScopeFieldsProps {
  idPrefix: string;
  groups: NodeGroup[];
  allNodes: boolean;
  onAllNodesChange: (v: boolean) => void;
  groupIds: string[];
  onGroupIdsChange: (v: string[]) => void;
  policyTypes: string[];
  onPolicyTypesChange: (v: string[]) => void;
  /** Id of the form error to point at when the group selection is invalid. */
  errorId?: string;
  groupsInvalid?: boolean;
}

export const ScopeFields: React.FC<ScopeFieldsProps> = ({
  idPrefix, groups, allNodes, onAllNodesChange, groupIds, onGroupIdsChange, policyTypes, onPolicyTypesChange, errorId, groupsInvalid,
}) => {
  const [groupsOpen, setGroupsOpen] = useState(false);
  const [typesOpen, setTypesOpen] = useState(false);
  const toggle = (list: string[], v: string) => (list.includes(v) ? list.filter((x) => x !== v) : [...list, v]);
  const groupName = (id: string) => groups.find((g) => g.id === id)?.name ?? id;

  return (
    <>
      <FormGroup label="Nodes" isRequired fieldId={`${idPrefix}-scope-all`} role="radiogroup">
        <Radio id={`${idPrefix}-scope-all`} name={`${idPrefix}-scope`} label="All nodes" isChecked={allNodes} onChange={() => onAllNodesChange(true)} />
        <Radio id={`${idPrefix}-scope-groups`} name={`${idPrefix}-scope`} label="Node groups" isChecked={!allNodes} onChange={() => onAllNodesChange(false)} />
      </FormGroup>
      {!allNodes && (
        <FormGroup label="Node groups" isRequired fieldId={`${idPrefix}-groups-toggle`}>
          <Select
            id={`${idPrefix}-groups`}
            role="menu"
            isOpen={groupsOpen}
            onOpenChange={setGroupsOpen}
            selected={groupIds}
            onSelect={(_ev, value) => onGroupIdsChange(toggle(groupIds, String(value)))}
            toggle={(ref: React.Ref<MenuToggleElement>) => (
              <MenuToggle
                id={`${idPrefix}-groups-toggle`}
                ref={ref}
                onClick={() => setGroupsOpen((v) => !v)}
                isExpanded={groupsOpen}
                isFullWidth
                aria-invalid={groupsInvalid ? true : undefined}
                aria-describedby={groupsInvalid && errorId ? errorId : undefined}
              >
                {groupIds.length === 0
                  ? "Select node groups"
                  : groupIds.length <= 3
                    ? groupIds.map(groupName).join(", ")
                    : `${groupIds.length} node groups`}
              </MenuToggle>
            )}
          >
            <SelectList aria-label="Node groups">
              {groups.length === 0 ? (
                <SelectOption isDisabled value="">No node groups</SelectOption>
              ) : (
                groups.map((g) => (
                  <SelectOption key={g.id} value={g.id} hasCheckbox isSelected={groupIds.includes(g.id)}>
                    {g.name}
                  </SelectOption>
                ))
              )}
            </SelectList>
          </Select>
          <FormHelperText>
            <HelperText>
              <HelperTextItem>Nodes that belong to any of the selected groups are included.</HelperTextItem>
            </HelperText>
          </FormHelperText>
        </FormGroup>
      )}
      <FormGroup label="Policy types" fieldId={`${idPrefix}-types-toggle`}>
        <Select
          id={`${idPrefix}-types`}
          role="menu"
          isOpen={typesOpen}
          onOpenChange={setTypesOpen}
          selected={policyTypes}
          onSelect={(_ev, value) => onPolicyTypesChange(toggle(policyTypes, String(value)))}
          toggle={(ref: React.Ref<MenuToggleElement>) => (
            <MenuToggle id={`${idPrefix}-types-toggle`} ref={ref} onClick={() => setTypesOpen((v) => !v)} isExpanded={typesOpen} isFullWidth>
              {policyTypes.length === 0 ? "All policy types" : `${policyTypes.length} of ${POLICY_TYPE_LIST.length} policy types`}
            </MenuToggle>
          )}
        >
          <SelectList aria-label="Policy types">
            {POLICY_TYPE_LIST.map((t) => (
              <SelectOption key={t.id} value={t.id} hasCheckbox isSelected={policyTypes.includes(t.id)}>
                {t.label}
              </SelectOption>
            ))}
          </SelectList>
        </Select>
        <FormHelperText>
          <HelperText>
            <HelperTextItem>Leave empty to include every policy type.</HelperTextItem>
          </HelperText>
        </FormHelperText>
      </FormGroup>
    </>
  );
};
