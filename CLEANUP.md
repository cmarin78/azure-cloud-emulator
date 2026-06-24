# CLEANUP.md — Tareas de ordenamiento para Claude

Notas de mantenimiento de higiene de repo, generadas tras una auditoría comparativa
con `gcp-emulator` y `ministack` (aws-emulator). No son features — son deuda de
housekeeping. Marcar con `[x]` a medida que se completen.

## Hecho en esta sesión

- [x] Eliminados del working dir (ya estaban correctamente en `.gitignore`, nunca
      llegaron a `git`, pero ensuciaban la carpeta): `azure-emulator.exe`,
      `azure-emulator.exe~`, `data.db`, y los ~15 archivos `*.log` sueltos en la
      raíz (`emu-err.log`, `smoke-out.log`, `phase8-err.log`, etc.), además de
      `.azure-emulator-data/` completo.

## Pendiente

### Higiene de repo
- [ ] Revisar `git log -- '**/*.tfstate'` para confirmar que ningún `terraform.tfstate`
      llegó a commitearse en el pasado (puede contener secretos en texto plano). Si
      apareciera, purgar del historial con `git filter-repo` o BFG, no solo borrar.
- [ ] Añadir un pre-commit hook o check en CI que falle si se intenta commitear un
      binario (`*.exe`) o un `.db` — para que este desorden no se repita.
- [ ] `bin/` está en `.gitignore` pero los binarios compilados localmente
      (`azure-emulator.exe`, `.exe~`) tienden a reaparecer ahí; documentar en el
      README que `bin/` es solo de build local, nunca para distribución.

### Documentación
- [ ] `ROADMAP.md` ya tiene 1020 líneas / 68KB y mezcla fases completadas con
      planificadas. Separar: crear `CHANGELOG.md` (estilo Keep a Changelog, como
      tiene `ministack`) con las fases 1-21 ya cerradas, y dejar `ROADMAP.md` solo
      con lo pendiente. Esto también facilita que un usuario nuevo entienda qué
      *ya funciona* sin leer 1000 líneas.
- [ ] Falta `CONTRIBUTING.md` y `SECURITY.md`. Si la intención es eventualmente
      aceptar contribuciones externas (como hace `ministack`), vale la pena
      definir ahora el proceso (cómo correr tests, estilo de commits, cómo
      reportar vulnerabilidades).

### CI / Calidad
- [ ] `ci.yml` solo corre `go build`, `go vet` y `go test -race`. No hay linter
      (`golangci-lint`) ni reporte de cobertura. `ministack` usa `ruff` + 
      `pytest-cov`; replicar el equivalente Go (`golangci-lint run` +
      `go test -coverprofile=coverage.out` con badge o reporte en el job).
- [ ] No hay step de `go mod verify` / `go mod tidy --diff` en CI para detectar
      dependencias huérfanas o `go.sum` desincronizado.

### Tests
- [ ] Servicios con varios archivos de implementación pero un solo archivo de test
      (la mayoría: `cosmosdb` 6 archivos/1 test, `keyvault` 6/1, `eventgrid` 6/1,
      `eventhub` 6/1, `monitor` 6/1, `servicebus` 6/1, `apimanagement` 5/1,
      `compute` 5/1) sugieren que la cobertura está concentrada en pocos
      escenarios "felices". Antes de seguir agregando fases nuevas, vale la pena
      pasar por estos servicios y sumar tests de error/edge-case.
- [ ] `network/` tiene 9 archivos de implementación y solo 2 de test — es el caso
      más desbalanceado; priorizarlo primero.

### Terraform / smoke tests
- [ ] `terraform/smoke-test/` y `terraform/azurerm-smoke-test/` quedan con
      `.terraform/` y `*.tfstate*` generados localmente cada vez que se corre el
      PoC. Ya están en `.gitignore`, pero documentar en el README de cada carpeta
      que hay que correr `terraform destroy` (o borrar el directorio) después de
      cada prueba para no acumular estado stale.

### Paridad entre los 3 proyectos
- [ ] `ministack` documenta cada fix en CHANGELOG con atribución de contribuidor
      y referencia a issue. Si el objetivo de `azure-emulator` es alcanzar un
      nivel de proyecto "publicable", adoptar esa misma disciplina de changelog
      por entrada, no solo por fase.
