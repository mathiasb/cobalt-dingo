# Git-workflow

## Remotes
| Remote   | URL                                                          | Syfte                              |
|----------|--------------------------------------------------------------|------------------------------------|
| `origin` | ssh://git@100.109.2.126:30022/mathias/coo-agent.git          | Kanonisk – allt arbete, alla releases |

GitHub-kopian (`mathiasb/coo-agent`) är arkiverad 2026-06-01 och read-only. Pusha aldrig dit.

## Dagligt arbete och releases → Gitea
```bash
git push origin main                              # vanlig push
git push origin feature/xyz                       # feature branch
git tag v1.2.0 -m "Release v1.2.0: kort beskrivning"
git push origin v1.2.0                            # tagg till Gitea Releases
```

## Branch-strategi
- `main` – alltid körbar, skyddad
- `feature/beskrivning` – ny funktionalitet
- `fix/beskrivning` – buggfixar
- `chore/beskrivning` – infrastruktur, dokumentation

## Commit-format (conventional commits)
```
feat: lägg till OAuth token refresh
fix: korrigera momsberäkning för MP2
chore: uppdatera beroenden
test: lägg till enhetstester för valideringsmotor
docs: uppdatera CLAUDE.md med nya BAS-konton
```

## Vad Claude aldrig pushar automatiskt
- Direkt till `main` utan att tester passerar
- Filer som matchar .gitignore (tokens, .env, loggar)
