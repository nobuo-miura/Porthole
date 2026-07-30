# Running Porthole on ECS / Fargate

> **Porthole has no authentication and can reach arbitrary internal destinations.**
> Never attach it to a public load balancer or give it a public IP. Run it for as long
> as you need to diagnose something, then remove it.

Two task definitions are provided. They are plain, valid task definitions — replace
every `<PLACEHOLDER>` and register them as-is.

| File | Mode | Use when |
|---|---|---|
| [sidecar-task-definition.json](sidecar-task-definition.json) | Sidecar in the app's own task | You need the app's exact network conditions |
| [standalone-task-definition.json](standalone-task-definition.json) | Separate one-off task | You want to avoid touching the app's task definition |

## Which mode to pick

### Sidecar — most accurate

In `awsvpc` mode every container in a task shares a single ENI, so a sidecar sees the
same subnet, the same security groups, the same route table and the same DNS resolver as
the app. Nothing has to be replicated, so nothing can be replicated wrongly.

It also answers a question the standalone mode cannot: because the network namespace is
shared, Porthole can probe the app's own listener over `localhost`. That distinguishes
"the app bound to `0.0.0.0`" from "the app bound only to `127.0.0.1`".

Costs: it needs a task definition revision and a redeploy, and you have to remember to
take it back out. In the example, Porthole is `essential: false` so that removing or
crashing it never kills the task, and it listens on **8081** because containers in a task
share the network namespace and cannot both bind 8080.

### Standalone — easier, but you must match the network yourself

```bash
aws ecs run-task \
  --cluster <CLUSTER> \
  --task-definition porthole \
  --launch-type FARGATE \
  --enable-execute-command \
  --network-configuration 'awsvpcConfiguration={
      subnets=[<SAME_SUBNET_AS_THE_APP>],
      securityGroups=[<THE_APPS_OWN_SECURITY_GROUP_ID>],
      assignPublicIp=DISABLED}'
```

**Attach the app's own security group, not a copy of it.** Security group rules are
normally written with another security group as the source — the database allows traffic
from `sg-app`, not from a CIDR. A different security group with byte-identical rules will
not satisfy such a rule, so the check fails even though the app connects fine.

Subnet matters too: route tables and network ACLs are attached per subnet, so pick a
subnet the app actually runs in.

## Reaching the UI without exposing it

There is no need to publish a port. ECS Exec bind-mounts an SSM agent into the task, and
Session Manager can forward a port through it.

For an ECS target, AWS documents the **`AWS-StartPortForwardingSessionToRemoteHost`**
document with a `host` parameter — not the plain `AWS-StartPortForwardingSession` used for
EC2 instances. Point `host` at `127.0.0.1` so the traffic lands on the container's own
listener.

**Standalone task** — Porthole listens on 8080:

```bash
aws ssm start-session \
  --target ecs:<CLUSTER>_<TASK_ID>_<RUNTIME_ID> \
  --document-name AWS-StartPortForwardingSessionToRemoteHost \
  --parameters '{"host":["127.0.0.1"],"portNumber":["8080"],"localPortNumber":["8080"]}'
```

**Sidecar** — Porthole listens on **8081** (8080 belongs to the app):

```bash
aws ssm start-session \
  --target ecs:<CLUSTER>_<TASK_ID>_<RUNTIME_ID> \
  --document-name AWS-StartPortForwardingSessionToRemoteHost \
  --parameters '{"host":["127.0.0.1"],"portNumber":["8081"],"localPortNumber":["8080"]}'
```

Either way, open <http://localhost:8080>.

Prerequisites:

- The task must run with `--enable-execute-command` (or the service must have
  `enableExecuteCommand: true`). **This is not a task definition field** — set it when you
  run the task or update the service:
  ```bash
  aws ecs update-service --cluster <CLUSTER> --service <SERVICE> \
    --enable-execute-command --force-new-deployment
  ```
- The **task role** (`taskRoleArn`) must allow `ssmmessages:CreateControlChannel`,
  `ssmmessages:CreateDataChannel`, `ssmmessages:OpenControlChannel` and
  `ssmmessages:OpenDataChannel`.
- Port forwarding to a remote host needs SSM Agent `3.1.1374.0` or later. The agent that
  ECS Exec injects on current Fargate platform versions satisfies this.
- Find `<RUNTIME_ID>` with:
  ```bash
  aws ecs describe-tasks --cluster <CLUSTER> --tasks <TASK_ID> \
    --query 'tasks[0].containers[?name==`porthole`].runtimeId' --output text
  ```
  Confirm the exec agent is up before connecting — `ExecuteCommandAgent` must be `RUNNING`:
  ```bash
  aws ecs describe-tasks --cluster <CLUSTER> --tasks <TASK_ID> \
    --query 'tasks[0].containers[?name==`porthole`].managedAgents' --output table
  ```

### Why `readonlyRootFilesystem` is `false`

A read-only root filesystem would be the safer default for a tool that never writes
files, but it is mutually exclusive with ECS Exec. From the
[ECS Exec documentation](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/ecs-exec.html):

> The SSM agent requires that the container file system can be written to in order to
> create the required directories and files. Therefore, making the root file system
> read-only using the `readonlyRootFilesystem` task definition parameter, or any other
> method, isn't supported.

Since ECS Exec is the access path recommended here — for both port forwarding and the CLI
— these task definitions set it to `false`. If you publish the port some other way and do
not need Exec at all, you can set it back to `true`.

`initProcessEnabled` is set for the same reason: AWS recommends it so the init process
reaps the SSM agent's zombie child processes.

## No browser? Use the CLI

ECS Exec gives you a shell, not a browser. The same checks run from the command line and
exit with a status code, which also makes them usable from CI/CD:

```bash
aws ecs execute-command --cluster <CLUSTER> --task <TASK_ID> \
  --container porthole --interactive --command "/usr/local/bin/porthole check --type tcp --host db.internal --port 5432"
```

Exit codes: `0` reachable, `1` definitively failed, `2` indeterminate, `3` bad usage.

## A warning about UDP results

A UDP check can only report three things, and in AWS the useful negative signal is
usually unavailable:

- **OK** — the peer sent a reply. Only protocols that answer (DNS, NTP, SNMP, …) do this.
- **FAIL** — an ICMP port-unreachable came back, so the port is definitively closed.
- **UNKNOWN** — nothing came back at all.

Security groups **drop** disallowed traffic instead of rejecting it, so a port blocked by
a security group produces no ICMP and looks exactly like an open port that simply does not
answer. In an AWS path, treat `UNKNOWN` as "no information", and prefer a TCP check
whenever the service offers one.
