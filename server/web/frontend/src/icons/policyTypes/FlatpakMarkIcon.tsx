// SPDX-License-Identifier: LGPL-3.0-or-later AND CC0-1.0
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors
//
// Icon artwork: "Flatpak" from Simple Icons v16.29.0
//   Source:  https://github.com/simple-icons/simple-icons/blob/develop/icons/flatpak.svg
//   Licence: CC0 1.0 Universal — see LICENSE.CC0-1.0.md in this directory
//   Trademark: the Flatpak mark belongs to the Flatpak project. It is used here only to
//   identify the technology a policy applies to (nominative use); see README.md.
// The createIcon wrapper below is Bor code (LGPL-3.0-or-later).

import { createIcon } from "@patternfly/react-icons/dist/esm/createIcon";

export const FlatpakMarkIcon = createIcon({
  name: "FlatpakMarkIcon",
  width: 24,
  height: 24,
  svgPath:
    "M12 0c-.556 0-1.111.144-1.61.432l-7.603 4.39a3.217 3.217 0 0 0-1.61 2.788v8.78c0 1.151.612 2.212 1.61 2.788l7.603 4.39a3.217 3.217 0 0 0 3.22 0l7.603-4.39a3.217 3.217 0 0 0 1.61-2.788V7.61a3.217 3.217 0 0 0-1.61-2.788L13.61.432A3.218 3.218 0 0 0 12 0Zm0 2.358c.15 0 .299.039.431.115l7.604 4.39c.132.077.24.187.315.316L12 12v9.642a.863.863 0 0 1-.431-.116l-7.604-4.39a.866.866 0 0 1-.431-.746V7.61c0-.153.041-.302.116-.43L12 12Z",
});

export default FlatpakMarkIcon;
