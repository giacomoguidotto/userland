# Userland

Userland describes the personal machine state that can be reproduced safely while keeping optional private identities separate from the public declaration.

## Language

**Realm**:
An optional operational identity whose configuration applies throughout one directory tree.
_Avoid_: Workspace, environment, context

**Realm declaration**:
A portable record that a realm exists and may be attached on a machine.
_Avoid_: Workspace configuration, mount

**Realm attachment**:
A machine-local opt-in that binds a realm declaration to its directory tree and enables its projections.
_Avoid_: Installation, checkout

**Realm projection**:
Machine-local generated configuration derived from an attached realm, such as a path-scoped Git identity or SSH alias.
_Avoid_: Global configuration, copied configuration

**Repository declaration**:
A desired canonical Git checkout at a realm-relative path on an explicitly owned branch.
_Avoid_: Submodule, project

**Repository taxonomy**:
The hierarchy formed by repository declaration paths inside a realm.
_Avoid_: Submodule tree, workspace layout

**Canonical checkout**:
The primary checkout owned by Userland. It is an exact, clean copy of a declared remote branch; feature work belongs in linked worktrees.
_Avoid_: Working copy, development checkout
