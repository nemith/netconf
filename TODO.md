# TODO
- [X] Upgrade transport based on versions (1.0->1.1 upgrade to chunked)
- [X] Cleanup request/response API (Session.Do, session.Call?)
- [X] Convert rpc errors to go errors
- [X] shutdown / close
- [X] all RFC6241 operations (methods + op structs?)
- [X] unit tests (>80% coverage?)
  - [X] operations
  - [X] server close, shutdown, etc
- [X] More linting
  - [X] renable errcheck
  - [X] errorlint
- [X] filter support
- [X] TLS support
- [X] Notification handler support
- [X] Capability query API
- [X] github actions (CI)

### Future
- [ ] benchmark against juniper/netconf / scrapligo
- [ ] Pool/SessionManager for automatic reconnects, retries, etc.
- [ ] Call Home support
- [ ] nccurl command to issue rpc requests from the cli
- [X] More RFC support 
  - [X] Partial Lock
  - [X] with-defaults
