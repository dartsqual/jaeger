# HotROD com Datadog dd-trace-go

Versão do [HotROD](../README.md) instrumentada com o tracer nativo da Datadog
([dd-trace-go v2](https://github.com/DataDog/dd-trace-go)), rodando em Kubernetes
e enviando traces para o Datadog Agent.

## O que mudou no código

- `pkg/tracing/init.go`: nova opção de exporter `datadog` (`--otel-exporter=datadog`).
  Em vez do SDK OpenTelemetry, o HotROD usa o **TracerProvider do dd-trace-go**
  (`ddtrace/opentelemetry`). Toda a instrumentação existente (otelhttp, otelgrpc e
  spans manuais) passa a gerar traces dd-trace, com propagação W3C entre os serviços.
- O tracer Datadog é global (um por processo): o primeiro serviço inicializado é o
  serviço principal (`DD_SERVICE` tem prioridade). Os "bancos" simulados no mesmo
  processo (`mysql` no customer, `redis-manual` no driver) recebem o atributo
  `service.name` em cada span e aparecem como serviços próprios
  (`DD_SERVICE_PREFIX` + nome, ex.: `hotrod-mysql`).
- `services/frontend/rum.go`: injeta o **Datadog Browser RUM** no `index.html`
  quando `DD_RUM_APPLICATION_ID` e `DD_RUM_CLIENT_TOKEN` estão definidos (ver
  [RUM](#rum-browser)). Sem essas variáveis, a página não muda.
- `cmd/flags.go`: texto de ajuda do `--otel-exporter` inclui `datadog`.
- `go.mod`/`go.sum`: dependência `github.com/DataDog/dd-trace-go/v2`.

Sem `--otel-exporter=datadog`, o comportamento original (OTLP/stdout) não muda.

## Build da imagem (sem Docker)

Requer Go 1.27+ (o toolchain é baixado automaticamente se necessário).

```bash
examples/hotrod/datadog/build-image.sh 1.0.2      # gera hotrod-dd.tar
sudo k3s ctr -n k8s.io images import hotrod-dd.tar # k3s; em outros clusters, faça push para um registry
```

## Deploy no Kubernetes

Um Deployment por microsserviço (`frontend`, `customer`, `driver`, `route`) no
namespace `hotrod`:

```bash
python3 examples/hotrod/datadog/k8s/gen_hotrod.py   # (re)gera k8s/hotrod.yaml (IMAGE/VERSION no topo do script)
kubectl apply -f examples/hotrod/datadog/k8s/hotrod.yaml
```

Pré-requisitos no cluster:

- Datadog Agent com APM habilitado (`datadog.apm.socketEnabled: true` ou porta 8126).
- Datadog Cluster Agent com **Admission Controller**: os pods têm o label
  `admission.datadoghq.com/enabled: "true"`, e o Cluster Agent injeta
  `DD_TRACE_AGENT_URL`/`DD_AGENT_HOST`, `DD_ENV`, `DD_SERVICE` e `DD_VERSION`
  (a partir dos labels `tags.datadoghq.com/*`).

Acesso externo (Traefik + Let's Encrypt), substituindo o hostname:

```bash
sed 's/HOTROD_HOST/hotrod.exemplo.com/' examples/hotrod/datadog/k8s/hotrod-ingress.yaml | kubectl apply -f -
```

## RUM (browser)

1. Crie uma aplicação RUM do tipo Browser no Datadog (ex.: `hotrod-web`) e coloque
   o Application ID em `RUM_APPLICATION_ID` no `gen_hotrod.py`.
2. Crie o Secret com o client token (o frontend o lê como opcional):

   ```bash
   kubectl -n hotrod create secret generic hotrod-rum --from-literal=client-token=pub...
   kubectl -n hotrod rollout restart deploy/frontend
   ```

| Variável | Padrão |
| --- | --- |
| `DD_RUM_APPLICATION_ID`, `DD_RUM_CLIENT_TOKEN` | obrigatórias |
| `DD_SITE` | `datadoghq.com` |
| `DD_RUM_SERVICE` | `hotrod-web` |
| `DD_ENV`, `DD_VERSION` | injetadas pelo Admission Controller |
| `DD_RUM_SESSION_SAMPLE_RATE` / `DD_RUM_SESSION_REPLAY_SAMPLE_RATE` | `100` / `100` |

As chamadas same-origin (`/dispatch`, `/config`) levam o header W3C `traceparent`,
então cada recurso RUM abre o trace do backend (`hotrod-web` → `hotrod-frontend`
→ customer/driver/route). Session Replay usa `mask-user-input`.

## No Datadog

- `env:hotrod`, serviços `hotrod-frontend`, `hotrod-customer`, `hotrod-driver`,
  `hotrod-route`, `hotrod-mysql`, `hotrod-redis-manual`.
- Um `GET /dispatch` gera um trace atravessando todos os serviços.
- Runtime metrics do Go habilitadas (`DD_RUNTIME_METRICS_ENABLED=true`).
- Os erros `redis timeout` no `hotrod-driver` são simulados pelo próprio HotROD.
