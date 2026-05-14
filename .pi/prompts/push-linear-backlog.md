Create Linear issues from `docs/linear-backlog.md` for the current project.

Workflow:
1. Read `docs/linear-backlog.md`.
2. Check Linear connection with `linear_status`; if it fails, ask me to run `/linear-set-key` once (saves to macOS Keychain) or restart Pi with `LINEAR_API_KEY` set.
3. Use `linear_list_teams` and ask me which Linear team/project to use if not obvious.
4. Create issues in Linear preserving title, priority, labels, problem, proposed fix, and acceptance criteria.
5. Prefer creating individual issues unless I say to group them.
6. After creating, summarize issue identifiers and links.
