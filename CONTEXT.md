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
A machine-local opt-in that binds a realm declaration to its directory tree.
_Avoid_: Installation, checkout

**Repository declaration**:
A desired Git checkout at a realm-relative path without ownership of its branch or revision.
_Avoid_: Submodule, project

**Repository taxonomy**:
The hierarchy formed by repository declaration paths inside a realm.
_Avoid_: Submodule tree, workspace layout
