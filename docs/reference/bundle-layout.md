# Bundle Layout

`deck pack` builds a self-contained bundle because disconnected work gets harder when dependencies stay implicit.

The bundle is part of the product model, not an afterthought.

## Typical bundle contents

- `workflows/`: the workflow files copied into the bundle
- `packages/`: operating system or Kubernetes packages fetched during pack
- `images/`: container image archives fetched during pack
- `files/`: supporting files copied or downloaded during pack
- `deck`: the `deck` binary placed in the bundle root
- `files/deck`: an additional bundled copy of the binary
- `.deck/manifest.json`: checksum metadata for bundled artifacts

## Why the bundle matters

- it keeps offline handoff explicit
- it reduces hidden runtime dependencies
- it makes the procedure easier to inspect before transport
- it supports the simple local execution model

## Core rule

If the site needs it to run the workflow, the safest default is to include it in the bundle rather than assume it already exists.
