# surreallog

## usage

```Dockerfile
FROM ghcr.io/tai-kun/surreallog:latest AS logger

FROM gcr.io/distroless/static

COPY --from=logger /surreallog /

ENTRYPOINT ["/surreallog", "<your command>", "[command arg]"]
```

```yaml
---
apiVersion: v1
kind: Pod
metadata:
  name: app
  namespace: default
spec:
  containers:
    - name: app
      image: <image>
      envFrom:
        - configMapRef:
            name: env
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: env
  namespace: default
data:
  SURREALLOG_ENDPOINT:  'ws://surrealdb.default.svc.cluster.local/rpc'
  SURREALLOG_USER:      'root'
  SURREALLOG_PASS:      'root'
  SURREALLOG_NAMESPACE: 'default'
---
apiVersion: v1
kind: Pod
metadata:
  name: surrealdb
  namespace: default
  labels:
    app: surrealdb
spec:
  containers:
    - name: surrealdb
      image: surrealdb/surrealdb:latest
      ports:
        - containerPort: 8000
      args: [
        "start",
        "--user", "root",
        "--pass", "root",
      ]
---
apiVersion: v1
kind: Service
metadata:
  name: surrealdb
  namespace: default
spec:
  selector:
    app: surrealdb
  ports:
    - protocol: TCP
      port: 80
      targetPort: 8000
  type: ClusterIP
```

## example

terminal(1):
```bash
docker run --rm --network host --pull always surrealdb/surrealdb:latest start -u root -p root --strict
```

terminal(2):
```bash
docker run --rm -it --network host surrealdb/surrealdb:latest sql -u root -p root --pretty
> DEFINE USER logger ON ROOT PASSWORD s'logger' ROLES EDITOR
```

terminal(3):
```bash
bash example/run.sh bash example/proc.sh
```

output:
```
2024/10/29 13:16:12 INFO completed with exit code 0
```

terminal(2):
```bash
> USE NS test
> INFO FOR NS
> USE DB <database>
> INFO FOR DB
```

terminal(2):
```bash
> SELECT * FROM `<table-number>` ORDER BY time
```

output:
```
-- Query 1 (execution time: 809.032µs)
[
  {
    id: ⟨1⟩:wbq1pbm2bf0zsvchlpbb,
    time: d'2024-10-29T04:16:09.312794442Z',
    kind: 1,
    text: 'tick: 0tick: 1'
  },
  {
    id: ⟨1⟩:er5wupdnf1fc1w0gmytj,
    time: d'2024-10-29T04:16:10.316087122Z',
    kind: 1,
    text: 'tick: 2'
  },
  {
    id: ⟨1⟩:ek8mfru9g7b4mebyj9j2,
    time: d'2024-10-29T04:16:12.322919475Z',
    kind: 1,
    text: 'tick: 3::add-mask::ichiro'
  },
  {
    id: ⟨1⟩:dt9p1j2ierun5lrgs7y2,
    time: d'2024-10-29T04:16:12.322924944Z',
    kind: 1,
    text: 'ichiro'
  },
  {
    id: ⟨1⟩:zxhcuy8hpcdrktcugfpq,
    time: d'2024-10-29T04:16:12.322949707Z',
    kind: 1,
    text: '1::add-mask::jiro'
  },
  {
    id: ⟨1⟩:9kjww7g41f99gbvv81ns,
    time: d'2024-10-29T04:16:12.322959914Z',
    kind: 1,
    text: 'jiro'
  },
  {
    id: ⟨1⟩:su851z0athw7kxx4d5ip,
    time: d'2024-10-29T04:16:12.323092485Z',
    kind: 1,
    text: '***'
  },
  {
    id: ⟨1⟩:t1li4z1i9qv7ruwu2xt7,
    time: d'2024-10-29T04:16:12.323110499Z',
    kind: 1,
    text: '***'
  },
  {
    id: ⟨1⟩:oi0xm0dkg5z8cpppow22,
    time: d'2024-10-29T04:16:12.323116002Z',
    kind: 1,
    text: '***'
  },
  {
    id: ⟨1⟩:h3o2ylzcbag5n68tgi8y,
    time: d'2024-10-29T04:16:12.323117460Z',
    kind: 1,
    text: ''
  },
  {
    id: ⟨1⟩:7y0tn648se32v881qyy9,
    time: d'2024-10-29T04:16:12.323127736Z',
    kind: 1,
    text: '***'
  },
  {
    id: ⟨1⟩:xlb58383np79nalfxmy8,
    time: d'2024-10-29T04:16:12.323132896Z',
    kind: 1,
    text: 'suzuki'
  },
  {
    id: ⟨1⟩:0h3q8q4a5xhcs5th5fid,
    time: d'2024-10-29T04:16:12.323250386Z',
    kind: -1,
    text: 'error',
    data: 'Wrong',
    opts: {
      file: 'foo.yml',
      line: 10,
      endLine: 1
    }
  },
  {
    id: ⟨1⟩:er6s0yiawy707o8wu6qx,
    time: d'2024-10-29T04:16:12.323259386Z',
    kind: -1,
    text: 'debug',
    data: 'ichiro'
  },
  {
    id: ⟨1⟩:zi3jhpn8nc26lenegoe5,
    time: d'2024-10-29T04:16:12.323263457Z',
    kind: -1,
    text: 'debug',
    data: '***'
  },
  {
    id: ⟨1⟩:3gxfy57ue1kooa2vbmjf,
    time: d'2024-10-29T04:16:12.323269769Z',
    kind: -1,
    text: 'group',
    data: 'title'
  },
  {
    id: ⟨1⟩:c11586dmkxuq3h54pzl5,
    time: d'2024-10-29T04:16:12.323272532Z',
    kind: -1,
    text: 'endgroup'
  },
  {
    id: ⟨1⟩:ndtpjez3s2eb3d8hitpp,
    time: d'2024-10-29T04:16:12.323274303Z',
    kind: -1,
    text: 'endgroup'
  }
]
```

terminal(2):
```bash
> SELECT * FROM catalog
```

output:
```
-- Query 1 (execution time: 222.835µs)
[
  {
    id: catalog:⟨1⟩,
    startedAt: d'2024-10-29T04:16:08.303656655Z',
    completedAt: d'2024-10-29T04:16:12.326835521Z',
    exitCode: 0
  }
]
```

## commands

See: https://docs.github.com/actions/writing-workflows/choosing-what-your-workflow-does/workflow-commands-for-github-actions?tool=bash
