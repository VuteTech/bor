// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

import React from "react";
import {
  Button,
  DescriptionList,
  DescriptionListDescription,
  DescriptionListGroup,
  DescriptionListTerm,
  List,
  ListItem,
} from "@patternfly/react-core";
import ExternalLinkAltIcon from "@patternfly/react-icons/dist/esm/icons/external-link-alt-icon";

import type { PolicyTypeDef } from "../../../policyTypes/registry";

/**
 * What a policy type does, changes and is good for — the text shared by the
 * Type step's side panel and the help drawer on the later steps.
 */
export const PolicyTypeDescriptionBody: React.FC<{ def: PolicyTypeDef }> = ({ def }) => (
  <>
    <p style={{ marginBottom: "1rem" }}>{def.description}</p>
    <DescriptionList isCompact>
      <DescriptionListGroup>
        <DescriptionListTerm>Manages on the node</DescriptionListTerm>
        <DescriptionListDescription>
          <List isPlain>
            {def.manages.map((m) => (
              <ListItem key={m}>
                <code style={{ fontSize: "0.85em", overflowWrap: "anywhere" }}>{m}</code>
              </ListItem>
            ))}
          </List>
        </DescriptionListDescription>
      </DescriptionListGroup>
      <DescriptionListGroup>
        <DescriptionListTerm>Applies to</DescriptionListTerm>
        <DescriptionListDescription>
          <List isPlain>
            {def.appliesTo.map((a) => (
              <ListItem key={a}>{a}</ListItem>
            ))}
          </List>
        </DescriptionListDescription>
      </DescriptionListGroup>
      <DescriptionListGroup>
        <DescriptionListTerm>Good for</DescriptionListTerm>
        <DescriptionListDescription>
          <List>
            {def.examples.map((e) => (
              <ListItem key={e}>{e}</ListItem>
            ))}
          </List>
        </DescriptionListDescription>
      </DescriptionListGroup>
    </DescriptionList>
    {def.docsHref && (
      <div style={{ marginTop: "1rem" }}>
        <Button
          component="a"
          href={def.docsHref}
          target="_blank"
          rel="noopener noreferrer"
          variant="link"
          isInline
          icon={<ExternalLinkAltIcon />}
          iconPosition="end"
        >
          {def.label} documentation
        </Button>
      </div>
    )}
  </>
);
