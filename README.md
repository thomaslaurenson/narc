# narc

![Build Status](https://img.shields.io/github/actions/workflow/status/UoA-eResearch/narc/tag.yml?style=flat) ![Test Status](https://img.shields.io/github/actions/workflow/status/UoA-eResearch/narc/tag.yml?style=flat&label=test)

![Release Version](https://img.shields.io/github/v/release/UoA-eResearch/narc?style=flat)

![Release downloads](https://img.shields.io/github/downloads/UoA-eResearch/narc/total?label=release_downloads)

The Nectar Access Rules Creator, or `narc`, is a tool to help construct OpenStack Access Rules for Application Credentials.

## What?!

- **Application Credentials** (AppCreds) allow software to authenticate to OpenStack without using a password
- **Access Rules** restrict an AppCred to only the exact API calls it needs
- Figuring out which access rules are needed is hard — most users fall back to "Unrestricted"
- `narc` intercepts your OpenStack API traffic, analyses it, and generates a ready-to-use `access_rules.json`

## Inspiration

[`iamlive`](https://github.com/iann0036/iamlive) is an amazing tool that shows AWS users the exact IAM permissions their CLI commands require. `narc` does the same thing for OpenStack.

## Installation

Download a pre-built binary from the [releases page](https://github.com/UoA-eResearch/narc/releases), or install from source:

```sh
go install github.com/thomaslaurenson/narc@latest
```

## Quickstart

### Wrap a command

`narc run` wraps a subprocess and intercepts all OpenStack API calls made during its lifetime:

```sh
narc run -- openstack server list
```

Results are written to `~/.narc/access_rules.json` when the wrapped command exits. Subprocess stdout is suppressed by default; use `--show-output` to see it.

### Background mode

Run the proxy in the background and configure your shell manually:

```sh
narc run --background
# narc prints the export commands to run in your shell, e.g.:
#   export https_proxy=http://127.0.0.1:9099
#   export HTTPS_PROXY=http://127.0.0.1:9099
#   export http_proxy=http://127.0.0.1:9099
#   export HTTP_PROXY=http://127.0.0.1:9099
#   export SSL_CERT_FILE=~/.narc/ca.pem
#   export REQUESTS_CA_BUNDLE=~/.narc/ca.pem
#   export OS_CACERT=~/.narc/ca.pem

# Run your tools…
openstack server list
terraform apply

# Stop narc with Ctrl-C; access_rules.json will be written on exit.
```

### Interactive shell session

`narc shell` launches your default shell with the proxy already configured. Run
as many commands as you like, then type `exit` or press Ctrl-D:

```sh
narc shell
# [narc] Proxy listening on http://127.0.0.1:9099
# [narc] Recording OpenStack API calls — run commands as normal.
# [narc] Shell: /bin/bash  |  Type 'exit' or press Ctrl-D to stop recording.
$ openstack server list
$ openstack network list
$ exit
# [narc] Done. 4 unique access rule(s) written to ~/.narc/access_rules.json
```

## Usage Examples

### OpenStack CLI

```sh
narc run -- openstack project list
```

### Terraform

```sh
narc run -- terraform apply
```

### Python (OpenStack SDK)

```sh
narc run -- python my_openstack_script.py
```

### Results

```json
[
    {
        "service": "identity",
        "method": "POST",
        "path": "/v3/auth/tokens"
    },
    {
        "service": "compute",
        "method": "GET",
        "path": "/v2.1/servers/**"
    }
]
```

## Configuration

`narc` stores its configuration in `~/.narc/narc.json`. The file is created with defaults on first run.

```json
{
    "proxy_port": 9099,
    "output_file": "~/.narc/access_rules.json",
    "log_file": "~/.narc/unmatched_requests.log"
}
```

## Environment Variables

When `narc run` wraps a subprocess, it injects the following into the child's environment:

| Variable | Value |
|---|---|
| `https_proxy` / `HTTPS_PROXY` | `http://127.0.0.1:<port>` |
| `http_proxy` / `HTTP_PROXY` | `http://127.0.0.1:<port>` |
| `SSL_CERT_FILE` | `~/.narc/ca.pem` |
| `REQUESTS_CA_BUNDLE` | `~/.narc/ca.pem` |
| `OS_CACERT` | `~/.narc/ca.pem` |

## CA Certificate

`narc` uses a local CA certificate to perform HTTPS interception (MITM). The certificate is generated automatically at `~/.narc/ca.pem` on first run and is valid for 2 years (auto-renewed when expiry is within 30 days). No manual setup is required.
