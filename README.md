# Proyecto 13 — Balanceo con Kubernetes (puro, sin NGINX)

Evolución de [`proyecto12 Load Balancer NGINX`](../proyecto%2012%20Load%20Balancer%20NGINX): en vez de un proceso NGINX configurado a mano, acá el balanceo lo hace **Kubernetes de forma nativa** — un `Service` reparte tráfico entre los Pods de un `Deployment`, sin Ingress ni NGINX de por medio. El monitor gráfico de escritorio (Ebitengine) se mantiene, y además recupera una capacidad que proyecto12 no podía tener sin montar el socket de Docker: **escalar los backends en caliente desde la propia GUI**, usando la API de Kubernetes.

## Arquitectura

```
GUI (nativa, Windows)                        Kubernetes (Kind, namespace loadbalancer-k8s)
  │  polling Postgres (localhost:5435)             │
  │  polling API generador (localhost:9091)        │
  ▼                                                  ▼
                                     generator ──▶ Service "backend" :8081 (kube-proxy balancea)
                                     (Deployment,        │
                                      1 réplica)   ┌─────┼─────┬─────...
                                                   ▼     ▼     ▼
                                              Pod backend (N réplicas, escalable en vivo)
                                                   │     │     │
                                                   └─────┴─────┘
                                                          ▼
                                                   Service "postgres"
                                                   (Deployment, 1 réplica + PVC)
```

Flujo de cada solicitud:
1. `generator` hace `POST http://backend:8081/process` (el nombre `backend` resuelve, vía DNS interno de Kubernetes, al `Service` — no a un Pod puntual) con header `X-Request-Id`.
2. El `Service backend` (ClusterIP) reparte la conexión entre los Pods `app=backend` que estén Ready. **Esto es lo que reemplaza a NGINX.**
3. El Pod que recibe genera una venta simulada, duerme el tiempo configurado (700ms por defecto, ajustable en vivo), la inserta en `ventas` usando su **propio nombre de Pod** como identidad (`os.Hostname()`), y responde con el header `X-Server-Id`.
4. `generator` lee `X-Server-Id` y registra la fila en `recepcion_transacciones` — igual que en proyecto12, porque ni un Service de Kubernetes ni NGINX pueden escribir a Postgres directamente.

## Diferencias importantes frente a proyecto12

- **Reparto pseudo-aleatorio, no round-robin limpio.** Kind usa kube-proxy en modo `iptables` por defecto: cada conexión nueva elige un Pod destino de forma pseudo-aleatoria (parejo en el agregado, pero no una secuencia 1-2-3-1-2-3 como hacía NGINX). Es una diferencia real y esperada, no un bug.
- **Bug evitado a propósito: keep-alive de HTTP rompe la demo.** Un Service de Kubernetes balancea **por conexión TCP**, no por request. El cliente HTTP del generador (`cmd/generator/main.go`, `backendHTTPClient`) usa `Transport{DisableKeepAlives: true}` a propósito — si reutilizara conexiones, casi todo el tráfico terminaría siempre en el mismo Pod (o en 1-2) aunque el Service esté perfectamente configurado.
- **Identidad = nombre del Pod, no un `SERVER_ID` fijo.** Por eso `servidor_id`/`servidor_asignado_id` en el esquema son `VARCHAR`, no `INT` como en proyecto12.
- **Escalado real** vía `POST /scale` en la API del generador (botones "REPLICAS -/+" en la GUI), que usa el subrecurso `/scale` del Deployment `backend` — lo mismo que hace `kubectl scale` por debajo.
- **Sin Docker Compose.** Todo es Kubernetes: Postgres, backend y generador son Deployments/Services, no contenedores de compose.

## Requisitos

- `kubectl` y `kind` instalados (usados: kubectl v1.32.2, kind v0.32.0)
- Docker Desktop
- Go 1.27+ (para compilar/correr la GUI nativa)

## Cómo correrlo

1. Crear el cluster dedicado (aislado de cualquier otro cluster Kind que ya tengas):

   ```powershell
   kind create cluster --config kind-config.yaml
   kubectl cluster-info
   ```

2. Construir las imágenes y cargarlas al cluster (Kind no usa un registry — `kind load` mete la imagen directo en el containerd del nodo):

   ```powershell
   docker build -f docker/Dockerfile.backend -t loadbalancer-k8s/backend:dev .
   docker build -f docker/Dockerfile.generator -t loadbalancer-k8s/generator:dev .
   kind load docker-image loadbalancer-k8s/backend:dev --name canales-lb
   kind load docker-image loadbalancer-k8s/generator:dev --name canales-lb
   ```

3. Aplicar los manifiestos (el orden alfabético de los archivos ya garantiza namespace → RBAC → deployments/services):

   ```powershell
   kubectl apply -f manifests/
   kubectl -n loadbalancer-k8s get pods -w
   ```

   Esperá a que Postgres, los 3 `backend-...` y `generator-...` queden `Running`/`1/1 Ready`.

4. Verificar que los NodePorts son alcanzables desde el host:

   ```powershell
   curl.exe http://localhost:9091/status
   ```

5. Copiar `.env.example` a `.env` (ya apunta a los puertos correctos) y correr la GUI:

   ```powershell
   copy .env.example .env
   go run ./cmd/gui
   ```

6. Presioná **START**, subí el RPM, y jugá con **REPLICAS +/-** — vas a ver aparecer/desaparecer cajas de Pod en tiempo real.

## Probar el escalado real

```powershell
curl.exe -X POST http://localhost:9091/scale -d "{\"delta\":2}"
kubectl -n loadbalancer-k8s get pods -w
```

Deberías ver Pods nuevos aparecer, y la GUI agregar cajas nuevas en su siguiente sondeo de `/pods` (~1s).

## Probar la auto-recuperación (a diferencia del failover pasivo de NGINX)

```powershell
kubectl -n loadbalancer-k8s get pods -l app=backend
kubectl -n loadbalancer-k8s delete pod <nombre-de-un-pod-backend>
```

Con tráfico corriendo, vas a ver que el Service sigue repartiendo entre los Pods restantes sin cortes, Kubernetes programa un reemplazo automáticamente (porque el Deployment exige N réplicas), y la caja correspondiente en la GUI se actualiza sola.

## Limpieza

```powershell
kind delete cluster --name canales-lb
```

## Diferencias clave con proyecto12

| | proyecto12 | proyecto13 |
|---|---|---|
| Balanceo | NGINX real, `upstream` round-robin | Service de Kubernetes, kube-proxy (pseudo-aleatorio) |
| Backends | 3 contenedores fijos (`backend1/2/3`) | Deployment con N réplicas, identidad = nombre de Pod |
| Escalado de servidores | Fijo (requeriría el socket de Docker) | Real, en vivo, vía la API de Kubernetes (botones REPLICAS +/-) |
| Failover | Pasivo (`max_fails`/`fail_timeout` en NGINX) | Auto-recuperación nativa de Kubernetes (el Deployment repone Pods) |
| Orquestación | Docker Compose | Manifiestos de Kubernetes (`kubectl apply`) |
| Tipo de `servidor_id` | `INT` | `VARCHAR(253)` (nombre de Pod) |
