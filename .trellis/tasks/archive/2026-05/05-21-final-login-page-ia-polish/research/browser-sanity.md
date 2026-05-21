# LoginPage browser sanity

## Attempt

Command attempted:

```bash
TMPDIR="$PWD/.tmp/playwright" python3 scripts/visual_evidence.py browser-sanity --base-url http://127.0.0.1:5178/ --route /login --viewport 1440x1000 --viewport 390x900
```

Preview server attempted:

```bash
npm --prefix web run dev -- --host 127.0.0.1 --port 5178
```

## Result

Blocked by local tooling:

```text
browser-sanity requires a locally installed Python Playwright package. The repository intentionally does not depend on it; install/use local browser tooling or record browser sanity as blocked.
```

No browser automation dependency was added to the repo. The preview server was stopped after the attempt.
