// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React, { useCallback, useId } from "react";
import {
  Button,
  Form,
  FormGroup,
  FormHelperText,
  HelperText,
  HelperTextItem,
  MenuToggle,
  MenuToggleElement,
  Select,
  SelectList,
  SelectOption,
  Switch,
  TextInput,
  Title,
} from "@patternfly/react-core";
import TrashIcon from "@patternfly/react-icons/dist/esm/icons/trash-icon";
import PlusCircleIcon from "@patternfly/react-icons/dist/esm/icons/plus-circle-icon";

import { LiveAlert } from "../../components/LiveAlert";

/* ── types ── */

interface FwPort {
  port: string;
  protocol: string;
}

interface FwForwardPort {
  port: string;
  protocol: string;
  toPort?: string;
  toAddr?: string;
}

interface FirewalldContent {
  zone?: string;
  target?: string;
  services?: string[];
  ports?: FwPort[];
  sourcePorts?: FwPort[];
  protocols?: string[];
  icmpBlocks?: string[];
  icmpBlockInversion?: boolean;
  masquerade?: boolean;
  forwardPorts?: FwForwardPort[];
  richRules?: string[];
}

const TARGET_OPTIONS: { value: string; label: string }[] = [
  { value: "default", label: "default (firewalld zone default)" },
  { value: "ACCEPT", label: "ACCEPT (accept unmatched packets)" },
  { value: "REJECT", label: "REJECT (reject with ICMP error)" },
  { value: "DROP", label: "DROP (silently drop unmatched packets)" },
];

const PROTOCOLS = ["tcp", "udp", "sctp", "dccp"];

/* ── helpers ── */

function parseContent(raw: string): FirewalldContent {
  try {
    const p = JSON.parse(raw || "{}") as Partial<FirewalldContent>;
    return {
      zone: typeof p.zone === "string" ? p.zone : "",
      target: typeof p.target === "string" ? p.target : "default",
      services: Array.isArray(p.services) ? p.services : [],
      ports: Array.isArray(p.ports) ? p.ports : [],
      sourcePorts: Array.isArray(p.sourcePorts) ? p.sourcePorts : [],
      protocols: Array.isArray(p.protocols) ? p.protocols : [],
      icmpBlocks: Array.isArray(p.icmpBlocks) ? p.icmpBlocks : [],
      icmpBlockInversion: !!p.icmpBlockInversion,
      masquerade: !!p.masquerade,
      forwardPorts: Array.isArray(p.forwardPorts) ? p.forwardPorts : [],
      richRules: Array.isArray(p.richRules) ? p.richRules : [],
    };
  } catch {
    return {};
  }
}

// serializeContent emits only the fields that carry data, so a policy that sets
// e.g. only services does not store empty arrays. The protojson decoder on the
// server accepts the camelCase keys.
function serializeContent(c: FirewalldContent): string {
  const out: FirewalldContent = {};
  if (c.zone) out.zone = c.zone;
  if (c.target && c.target !== "default") out.target = c.target;
  if (c.services && c.services.length) out.services = c.services;
  if (c.ports && c.ports.length) out.ports = c.ports;
  if (c.sourcePorts && c.sourcePorts.length) out.sourcePorts = c.sourcePorts;
  if (c.protocols && c.protocols.length) out.protocols = c.protocols;
  if (c.icmpBlocks && c.icmpBlocks.length) out.icmpBlocks = c.icmpBlocks;
  if (c.icmpBlockInversion) out.icmpBlockInversion = true;
  if (c.masquerade) out.masquerade = true;
  if (c.forwardPorts && c.forwardPorts.length) out.forwardPorts = c.forwardPorts;
  if (c.richRules && c.richRules.length) out.richRules = c.richRules;
  return JSON.stringify(out, null, 2);
}

/* ── reusable string-list editor ── */

interface StringListProps {
  label: string;
  values: string[];
  placeholder: string;
  addLabel: string;
  onChange: (vals: string[]) => void;
  isDisabled?: boolean;
  idPrefix: string;
  helper?: React.ReactNode;
}

const StringListEditor: React.FC<StringListProps> = ({
  label, values, placeholder, addLabel, onChange, isDisabled, idPrefix, helper,
}) => (
  <FormGroup label={label} fieldId={idPrefix}>
    {values.map((v, idx) => (
      <div key={idx} style={{ display: "flex", gap: "0.5rem", marginBottom: "0.4rem", alignItems: "center" }}>
        <TextInput
          id={`${idPrefix}-${idx}`}
          value={v}
          onChange={(_ev, nv) => onChange(values.map((x, i) => (i === idx ? nv : x)))}
          placeholder={placeholder}
          isDisabled={isDisabled}
          aria-label={`${label} ${idx + 1}`}
        />
        <Button
          variant="plain"
          onClick={() => onChange(values.filter((_, i) => i !== idx))}
          isDisabled={isDisabled}
          aria-label={`Remove ${label} ${idx + 1}`}
          style={{ color: "var(--pf-t--global--color--status--danger--100)" }}
        >
          <TrashIcon />
        </Button>
      </div>
    ))}
    <Button
      variant="secondary"
      icon={<PlusCircleIcon />}
      onClick={() => onChange([...values, ""])}
      isDisabled={isDisabled}
      size="sm"
    >
      {addLabel}
    </Button>
    {helper && (
      <FormHelperText>
        <HelperText><HelperTextItem>{helper}</HelperTextItem></HelperText>
      </FormHelperText>
    )}
  </FormGroup>
);

/* ── reusable protocol select ── */

const ProtocolSelect: React.FC<{
  value: string;
  onChange: (v: string) => void;
  isDisabled?: boolean;
  id: string;
  ariaLabel: string;
}> = ({ value, onChange, isDisabled, id, ariaLabel }) => {
  const [open, setOpen] = React.useState(false);
  return (
    <Select
      id={id}
      isOpen={open}
      onOpenChange={setOpen}
      selected={value}
      onSelect={(_ev, val) => { onChange(val as string); setOpen(false); }}
      toggle={(ref: React.Ref<MenuToggleElement>) => (
        <MenuToggle ref={ref} onClick={() => setOpen(v => !v)} isExpanded={open} isDisabled={isDisabled} aria-label={ariaLabel}>
          {value || "tcp"}
        </MenuToggle>
      )}
    >
      <SelectList>
        {PROTOCOLS.map(p => <SelectOption key={p} value={p}>{p}</SelectOption>)}
      </SelectList>
    </Select>
  );
};

/* ── reusable port-list editor ── */

const PortListEditor: React.FC<{
  label: string;
  values: FwPort[];
  onChange: (vals: FwPort[]) => void;
  isDisabled?: boolean;
  idPrefix: string;
}> = ({ label, values, onChange, isDisabled, idPrefix }) => (
  <FormGroup label={label} fieldId={idPrefix}>
    {values.map((p, idx) => (
      <div key={idx} style={{ display: "flex", gap: "0.5rem", marginBottom: "0.4rem", alignItems: "center" }}>
        <TextInput
          id={`${idPrefix}-port-${idx}`}
          value={p.port}
          onChange={(_ev, v) => onChange(values.map((x, i) => (i === idx ? { ...x, port: v } : x)))}
          placeholder="443 or 5000-6000"
          isDisabled={isDisabled}
          aria-label={`${label} ${idx + 1} port`}
          style={{ maxWidth: "12rem" }}
        />
        <ProtocolSelect
          id={`${idPrefix}-proto-${idx}`}
          value={p.protocol || "tcp"}
          onChange={(v) => onChange(values.map((x, i) => (i === idx ? { ...x, protocol: v } : x)))}
          isDisabled={isDisabled}
          ariaLabel={`${label} ${idx + 1} protocol`}
        />
        <Button
          variant="plain"
          onClick={() => onChange(values.filter((_, i) => i !== idx))}
          isDisabled={isDisabled}
          aria-label={`Remove ${label} ${idx + 1}`}
          style={{ color: "var(--pf-t--global--color--status--danger--100)" }}
        >
          <TrashIcon />
        </Button>
      </div>
    ))}
    <Button
      variant="secondary"
      icon={<PlusCircleIcon />}
      onClick={() => onChange([...values, { port: "", protocol: "tcp" }])}
      isDisabled={isDisabled}
      size="sm"
    >
      Add port
    </Button>
  </FormGroup>
);

/* ── main component ── */

export interface FirewalldPolicyEditorProps {
  contentRaw: string;
  onChange: (newRaw: string) => void;
  isDisabled?: boolean;
}

export const FirewalldPolicyEditor: React.FC<FirewalldPolicyEditorProps> = ({
  contentRaw, onChange, isDisabled,
}) => {
  const idPrefix = useId();
  const [targetOpen, setTargetOpen] = React.useState(false);

  const content = parseContent(contentRaw);
  const push = useCallback(
    (updated: FirewalldContent) => onChange(serializeContent(updated)),
    [onChange],
  );

  const currentTargetLabel =
    TARGET_OPTIONS.find(t => t.value === (content.target || "default"))?.label ?? content.target;

  return (
    <Form>
      <Title headingLevel="h4" size="md">Zone</Title>

      <FormGroup label="Target zone" fieldId={`${idPrefix}-zone`}>
        <TextInput
          id={`${idPrefix}-zone`}
          value={content.zone ?? ""}
          onChange={(_ev, v) => push({ ...content, zone: v })}
          placeholder="leave empty for the node's default zone"
          isDisabled={isDisabled}
          aria-label="Target firewalld zone"
        />
        <FormHelperText>
          <HelperText>
            <HelperTextItem>
              The firewalld zone whose ruleset Bor manages. Empty = the node&apos;s
              default zone. Host-specific interface/source bindings are preserved.
            </HelperTextItem>
          </HelperText>
        </FormHelperText>
      </FormGroup>

      <FormGroup label="Default target" fieldId={`${idPrefix}-target`}>
        <Select
          id={`${idPrefix}-target`}
          isOpen={targetOpen}
          onOpenChange={setTargetOpen}
          selected={content.target || "default"}
          onSelect={(_ev, val) => { push({ ...content, target: val as string }); setTargetOpen(false); }}
          toggle={(ref: React.Ref<MenuToggleElement>) => (
            <MenuToggle ref={ref} onClick={() => setTargetOpen(v => !v)} isExpanded={targetOpen} isDisabled={isDisabled} aria-label="Select default target">
              {currentTargetLabel}
            </MenuToggle>
          )}
        >
          <SelectList>
            {TARGET_OPTIONS.map(t => <SelectOption key={t.value} value={t.value}>{t.label}</SelectOption>)}
          </SelectList>
        </Select>
      </FormGroup>

      <Title headingLevel="h4" size="md" style={{ marginTop: "0.5rem" }}>Allowed traffic</Title>

      <StringListEditor
        label="Services"
        values={content.services ?? []}
        placeholder="e.g. ssh, http, https"
        addLabel="Add service"
        onChange={(vals) => push({ ...content, services: vals })}
        isDisabled={isDisabled}
        idPrefix={`${idPrefix}-svc`}
        helper={<>Named firewalld services (run <code>firewall-cmd --get-services</code> to list).</>}
      />

      <PortListEditor
        label="Ports"
        values={content.ports ?? []}
        onChange={(vals) => push({ ...content, ports: vals })}
        isDisabled={isDisabled}
        idPrefix={`${idPrefix}-port`}
      />

      <StringListEditor
        label="Protocols"
        values={content.protocols ?? []}
        placeholder="e.g. gre, esp, ah"
        addLabel="Add protocol"
        onChange={(vals) => push({ ...content, protocols: vals })}
        isDisabled={isDisabled}
        idPrefix={`${idPrefix}-proto`}
        helper={<>Protocol names from <code>/etc/protocols</code>.</>}
      />

      <PortListEditor
        label="Source ports"
        values={content.sourcePorts ?? []}
        onChange={(vals) => push({ ...content, sourcePorts: vals })}
        isDisabled={isDisabled}
        idPrefix={`${idPrefix}-sport`}
      />

      <Title headingLevel="h4" size="md" style={{ marginTop: "0.5rem" }}>ICMP &amp; NAT</Title>

      <StringListEditor
        label="ICMP blocks"
        values={content.icmpBlocks ?? []}
        placeholder="e.g. echo-request"
        addLabel="Add ICMP block"
        onChange={(vals) => push({ ...content, icmpBlocks: vals })}
        isDisabled={isDisabled}
        idPrefix={`${idPrefix}-icmp`}
        helper={<>ICMP types to block (run <code>firewall-cmd --get-icmptypes</code> to list).</>}
      />

      <FormGroup label="ICMP block inversion" fieldId={`${idPrefix}-icmp-inv`}>
        <Switch
          id={`${idPrefix}-icmp-inv`}
          label="Accept only the listed ICMP types; block all others"
          isChecked={!!content.icmpBlockInversion}
          onChange={(_ev, checked) => push({ ...content, icmpBlockInversion: checked })}
          isDisabled={isDisabled}
        />
      </FormGroup>

      <FormGroup label="Masquerade (NAT)" fieldId={`${idPrefix}-masq`}>
        <Switch
          id={`${idPrefix}-masq`}
          label="Enable IP masquerading for this zone"
          isChecked={!!content.masquerade}
          onChange={(_ev, checked) => push({ ...content, masquerade: checked })}
          isDisabled={isDisabled}
        />
      </FormGroup>

      <Title headingLevel="h4" size="md" style={{ marginTop: "0.5rem" }}>Port forwarding</Title>

      <FormGroup label="Forward ports" fieldId={`${idPrefix}-fwd`}>
        {(content.forwardPorts ?? []).map((f, idx) => (
          <div key={idx} style={{ display: "flex", gap: "0.5rem", marginBottom: "0.4rem", alignItems: "center", flexWrap: "wrap" }}>
            <TextInput
              id={`${idPrefix}-fwd-port-${idx}`}
              value={f.port}
              onChange={(_ev, v) => push({ ...content, forwardPorts: (content.forwardPorts ?? []).map((x, i) => (i === idx ? { ...x, port: v } : x)) })}
              placeholder="from port"
              isDisabled={isDisabled}
              aria-label={`Forward ${idx + 1} source port`}
              style={{ maxWidth: "9rem" }}
            />
            <ProtocolSelect
              id={`${idPrefix}-fwd-proto-${idx}`}
              value={f.protocol || "tcp"}
              onChange={(v) => push({ ...content, forwardPorts: (content.forwardPorts ?? []).map((x, i) => (i === idx ? { ...x, protocol: v } : x)) })}
              isDisabled={isDisabled}
              ariaLabel={`Forward ${idx + 1} protocol`}
            />
            <TextInput
              id={`${idPrefix}-fwd-toport-${idx}`}
              value={f.toPort ?? ""}
              onChange={(_ev, v) => push({ ...content, forwardPorts: (content.forwardPorts ?? []).map((x, i) => (i === idx ? { ...x, toPort: v || undefined } : x)) })}
              placeholder="to port (optional)"
              isDisabled={isDisabled}
              aria-label={`Forward ${idx + 1} destination port`}
              style={{ maxWidth: "10rem" }}
            />
            <TextInput
              id={`${idPrefix}-fwd-toaddr-${idx}`}
              value={f.toAddr ?? ""}
              onChange={(_ev, v) => push({ ...content, forwardPorts: (content.forwardPorts ?? []).map((x, i) => (i === idx ? { ...x, toAddr: v || undefined } : x)) })}
              placeholder="to address (optional)"
              isDisabled={isDisabled}
              aria-label={`Forward ${idx + 1} destination address`}
              style={{ maxWidth: "12rem" }}
            />
            <Button
              variant="plain"
              onClick={() => push({ ...content, forwardPorts: (content.forwardPorts ?? []).filter((_, i) => i !== idx) })}
              isDisabled={isDisabled}
              aria-label={`Remove forward port ${idx + 1}`}
              style={{ color: "var(--pf-t--global--color--status--danger--100)" }}
            >
              <TrashIcon />
            </Button>
          </div>
        ))}
        <Button
          variant="secondary"
          icon={<PlusCircleIcon />}
          onClick={() => push({ ...content, forwardPorts: [...(content.forwardPorts ?? []), { port: "", protocol: "tcp" }] })}
          isDisabled={isDisabled}
          size="sm"
        >
          Add forward port
        </Button>
      </FormGroup>

      <Title headingLevel="h4" size="md" style={{ marginTop: "0.5rem" }}>Rich rules</Title>

      <StringListEditor
        label="Rich rules"
        values={content.richRules ?? []}
        placeholder={`rule family="ipv4" source address="10.0.0.0/8" service name="ssh" accept`}
        addLabel="Add rich rule"
        onChange={(vals) => push({ ...content, richRules: vals })}
        isDisabled={isDisabled}
        idPrefix={`${idPrefix}-rich`}
        helper={
          <>
            firewalld rich-language rules for cases not covered above. Each must
            begin with <code>rule</code>. Invalid rules are rejected by{" "}
            <code>firewall-cmd --check-config</code> on the node.
          </>
        }
      />

      {!content.zone && !content.target && (content.services ?? []).length === 0 &&
        (content.ports ?? []).length === 0 && (content.richRules ?? []).length === 0 && (
        <LiveAlert
          message="This firewall policy is currently empty. Add at least one service, port, or rich rule."
          variant="info"
        />
      )}
    </Form>
  );
};
