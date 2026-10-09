@/Users/daodao/.codex/RTK.md

## Build and runtime operations

Read [docs/development.md](docs/development.md) before building, installing, restarting or testing against a device. It contains the production build flags, session-preserving update sequence, existing signing requirements and isolated test commands.

- Use `GOWORK=off` for release builds and published-SDK validation. A local MyGo workspace is for explicit framework development.
- MyGo comes from the fork `github.com/daodao97/mygo` at a pseudo-version of its `main`. Land MyGo changes on that `main` and push before bumping `go.mod`; never pin a feature branch, an unpushed commit or a local path. `../mygo` (used by `go.work`) must stay on `main` at the pinned commit. Run `./scripts/check-mygo.sh` before committing dependency changes or releasing. See "MyGo 依赖" in docs/development.md.
- Manual desktop releases require both `-tags mygo_noinspector` and `-ldflags '-X github.com/egoist/mygo.production=1'`. Plain `go build` defaults to MyGo development mode.
- Keep development and test data separate from the installed application's data. Do not run `mygo dev` against real sessions.
- Preserve the live session daemon, push worker, Agent CLIs and shared Codex daemon. Never batch-kill GoRex processes or delete real sockets/state to fix an update. Quit only the GUI normally.
- Record and compare daemon PID, session IDs, shell PIDs and terminal dimensions before and after a GUI update, after initialization has completed. Do not restart a live service to activate new code while tasks are running.
- Reuse the phone's existing Team, Bundle ID and provisioning profile. Overwrite-install the app; do not uninstall the normal app or switch signing teams as a troubleshooting shortcut.
- Run remote/device tests against disposable fixtures, then launch the normal mobile app without `GOREX_UI_TEST` and clean up only owned test resources.
