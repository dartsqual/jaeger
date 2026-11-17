"""Gera hotrod.yaml: HotROD com dd-trace-go, um Deployment + Service por microsserviço."""

SERVICES = [
    ("frontend", 8080, ["--customer-service-hostname=customer",
                        "--driver-service-hostname=driver",
                        "--route-service-hostname=route"]),
    ("customer", 8081, []),
    ("driver", 8082, []),
    ("route", 8083, []),
]
IMAGE = "docker.io/library/hotrod-dd:1.0.2"
ENV = "hotrod"
# Datadog RUM no frontend: application ID (público) + client token no Secret
# hotrod/hotrod-rum (chave client-token). Secret opcional: sem ele o RUM fica desligado.
RUM_APPLICATION_ID = "58ed9bf0-b54e-4749-976e-0c4d6d7d242d"
VERSION = "1.0.2"

docs = ["""# HotROD (jaegertracing/jaeger/examples/hotrod) instrumentado com Datadog dd-trace-go.
# Imagem hotrod-dd (ver ../README.md), build com o TracerProvider do dd-trace-go (flag -x datadog).
# Um Deployment por microsserviço: no dd-trace, 1 processo = 1 serviço.
# O Datadog Admission Controller injeta a conexão com o Agent (socket APM) e
# DD_ENV/DD_SERVICE/DD_VERSION a partir dos labels tags.datadoghq.com/*.
apiVersion: v1
kind: Namespace
metadata:
  name: hotrod
"""]

RUM_ENV = f"""            - name: DD_RUM_APPLICATION_ID
              value: {RUM_APPLICATION_ID}
            - name: DD_RUM_SERVICE
              value: hotrod-web
            - name: DD_RUM_CLIENT_TOKEN
              valueFrom:
                secretKeyRef:
                  name: hotrod-rum
                  key: client-token
                  optional: true
"""

for name, port, extra in SERVICES:
    svc = f"hotrod-{name}"
    rum_env = RUM_ENV if name == "frontend" else ""
    args = "\n".join(f"            - {a}" for a in [name, "--otel-exporter=datadog", *extra])
    docs.append(f"""apiVersion: apps/v1
kind: Deployment
metadata:
  name: {name}
  namespace: hotrod
  labels:
    app.kubernetes.io/name: {name}
    app.kubernetes.io/part-of: hotrod
    tags.datadoghq.com/env: {ENV}
    tags.datadoghq.com/service: {svc}
    tags.datadoghq.com/version: "{VERSION}"
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: {name}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: {name}
        app.kubernetes.io/part-of: hotrod
        tags.datadoghq.com/env: {ENV}
        tags.datadoghq.com/service: {svc}
        tags.datadoghq.com/version: "{VERSION}"
        admission.datadoghq.com/enabled: "true"
      annotations:
        ad.datadoghq.com/{name}.logs: '[{{"source": "go", "service": "{svc}"}}]'
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
      containers:
        - name: {name}
          image: {IMAGE}
          imagePullPolicy: Never
          command: ["/usr/local/bin/hotrod"]
          args:
{args}
          env:
            - name: DD_SERVICE
              value: {svc}
            - name: DD_SERVICE_PREFIX
              value: hotrod-
            - name: DD_RUNTIME_METRICS_ENABLED
              value: "true"
            - name: DD_TRACE_SAMPLE_RATE
              value: "1"
{rum_env}          ports:
            - name: http
              containerPort: {port}
          readinessProbe:
            tcpSocket:
              port: {port}
            initialDelaySeconds: 3
            periodSeconds: 10
          resources:
            requests:
              cpu: 20m
              memory: 48Mi
            limits:
              memory: 192Mi
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
""")
    docs.append(f"""apiVersion: v1
kind: Service
metadata:
  name: {name}
  namespace: hotrod
spec:
  selector:
    app.kubernetes.io/name: {name}
  ports:
    - name: http
      port: {port}
      targetPort: {port}
""")

open(__import__("os").path.join(__import__("os").path.dirname(__file__), "hotrod.yaml"), "w").write("---\n".join(docs))
print("ok")
