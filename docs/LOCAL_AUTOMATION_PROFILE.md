# Local Automation Profile (PowerShell)

This repository standardizes local daily development around your PowerShell profile workflow.

## Required behavior

1. Do not run automation directly on `main`, `develop`, or `master`.
2. Before pushing, run local quality checks (`go test -count=1 ./...` for Go projects).
3. Push feature branch -> create PR -> attach auto-merge.
4. Prefer squash when branch has 0-1 new commit; choose squash/rebase interactively when commit count is larger.

## Reference profile script

The exact profile automation script is stored at:

- `docs/snippets/powershell_profile_gpa.ps1`

If you update your local profile logic, update that file in the same PR so repository docs stay in sync.

## Collaboration contract for this repo

When collaborating on this repository, automation and release operations should align with the `gpa` flow above.
If a tool environment cannot execute `gh` commands (for example missing CLI in PATH), operations should fall back to manual GitHub UI steps while keeping the same branch/PR/auto-merge policy.
