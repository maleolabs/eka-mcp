# Audit levels

L0: `auditNonEKAPathLevel(root, "L0")` — fast, file list only.
L1/L2: `auditNonEKAPathLevel(root, "L1")` — deep: docs + codegraph + redaction, sample 50, docs 5, codegraph 5.
Redaction: skip `.env`, `secret`, `.pem`.
Hash: sha256(fileList + totalBytes + level)[:16].
