"""Gera hotrod.yaml: HotROD com dd-trace-go, um Deployment + Service por microsserviço."""

SERVICES = [
    ("frontend", 8080, ["--customer-service-hostname=customer",
                        "--driver-service-hostname=driver",
                        "--route-service-hostname=route"]),
    ("customer", 8081, []),
    ("driver", 8082, []),
    ("route", 8083, []),
]
IMAGE = "docker.io/library/hotrod-dd:1.0.1"
ENV = "hotrod"
VERSION = "1.0.1"

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

for name, port, extra in SERVICES:
    svc = f"hotrod-{name}"
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
          ports:
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
