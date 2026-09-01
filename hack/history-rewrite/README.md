# One-time history rewrite

The audit trail for the history rewrite performed once, before this repository
was made public. It is kept so the operation is reproducible and reviewable from
the repository alone; **it is not part of any routine workflow and must not be
re-run.**

## What it did

| Callback | Effect |
|---|---|
| [`blob_cb.py`](blob_cb.py) | Replaced the body of every `-----BEGIN … PRIVATE KEY-----` block in every historical blob with a redaction marker. Two demo SSH keys in `samples/` were affected (findings F-01, F-02). |
| [`msg_cb.py`](msg_cb.py) | Dropped AI-assistant attribution trailers from three commit messages (F-03), and replaced a live shoot name, a cluster name and two links into a private issue tracker with placeholders (F-07). |

Tree content was otherwise byte-for-byte identical: the only file differences
across the whole history are the two redacted key blocks.

[`apply-history-rewrite.sh`](apply-history-rewrite.sh) drives
[git-filter-repo](https://github.com/newren/git-filter-repo) with those two
callbacks against a mirror clone, verifies the result, and prints the push
command rather than pushing.

See [`docs/open-source-readiness.md`](../../docs/open-source-readiness.md) for
the full scan report and the publication runbook.

## Consequence

Every commit SHA changed. The `commit/<sha>` and `compare/<sha>...<sha>` links in
`CHANGELOG.md` therefore point at commits that no longer exist. The old-to-new
SHA translation produced by the rewrite (`filter-repo/commit-map` in the
rewritten mirror) is the only way to repair them, and is preserved with the
publication ticket.
