# Roadmap

What is planned for Zero2Dev, in the order it is being built. Each stage is tested before the next starts.

## 0.0.3: databases, code editors and database tools

Goal: install and run the rest of a typical local stack from the same place, without touching the system.

| Stage | What | Status |
| --- | --- | --- |
| 0 | Version 0.0.3, this roadmap | done |
| 1 | MD5 checksums (MySQL publishes only MD5) | done |
| 2 | New kinds and categories: Databases, Code editors, Database tools | done |
| 3 | Download sources for the new kinds | done |
| 4 | **Open** editors and database tools (on a project folder or a database) | done |
| 5 | Databases as services: data folder, first-start setup, safe stop, connection details | done |
| 6 | Mock daemon, docs and end-to-end tests | done |

### What ships

| Category | Tools | Download and check |
| --- | --- | --- |
| Databases | MariaDB | mariadb.org API, SHA-256 |
| | MySQL | dev.mysql.com, **MD5** (the only checksum MySQL publishes) |
| | PostgreSQL | EnterpriseDB binaries ZIP, no checksum published (you confirm, like nginx) |
| | Redis | community Windows build (github.com/redis-windows), SHA-256 |
| Code editors | VS Code | Microsoft update API, SHA-256, portable mode |
| | VSCodium | GitHub releases, SHA-256, portable mode |
| | Notepad++ | GitHub releases (portable ZIP), SHA-256 |
| Database tools | HeidiSQL | GitHub releases (portable ZIP), SHA-256 |
| | DBeaver CE | GitHub releases, SHA-256, workspace kept in the data folder |

### How databases behave

- Data lives in `data\databases\<service id>` and moves with the folder. Deleting a service keeps its data.
- The first start creates the database files (`mariadb-install-db`, `mysqld --initialize-insecure`, `initdb`).
- They only listen on `127.0.0.1`. The administrator account has no password (`root` for MariaDB/MySQL, `postgres` for PostgreSQL), which is fine for a local development machine and must never be exposed.
- **Stop** runs the database's own shutdown command (`mysqladmin shutdown`, `pg_ctl stop`, `redis-cli shutdown`) before anything is killed, so data files are not damaged.
- The service page shows the connection details and a copyable URL, plus **Open in HeidiSQL / DBeaver** when those are installed.

### How editors and tools behave

- They are not services: they are opened, not supervised, and keep running when Zero2Dev stops.
- VS Code and VSCodium run in portable mode, so settings and extensions stay inside the `bin` folder.
- A service page has **Open in** to open its project folder in an installed editor.

## Later

- Ports 80/443 and hosts-file domains
- Database templates (a database plus a site plus a service in one step)
- Auto-update and code signing
