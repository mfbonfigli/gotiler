#!/usr/bin/env bash
set -Eeuo pipefail

REGION="eu-west-1"
INPUT_REGION="eu-west-1"
INSTANCE_TYPE="i4i.2xlarge"
AMI_SSM_PARAM="/aws/service/canonical/ubuntu/server/24.04/stable/current/amd64/hvm/ebs-gp3/ami-id"
AMI_ID=""
INPUT_S3="s3://mfb-data-files/Helsinki_RGB_Class_TM35FIN_EPSG3067.las"
INPUT_EPSG="3067"

PY3DTILES_VERSION="12.1.1"
MAGO_VERSION="1.16.4"
MAGO_URL="https://github.com/Gaia3D/mago-3d-tiler/releases/download/v1.16.4/mago-3d-tiler-1.16.4.jar"
GOTILER_REPO="mfbonfigli/gotiler"
GOTILER_VERSION="v3.2.0"
GOTILER_URL="${GOTILER_URL:-}"
GOTILER_LOCAL_DIR=""
GOTILER_ASSET_REGEX="(gotiler|gocesiumtiler).*((linux|lin).*(amd64|x64)|(amd64|x64).*(linux|lin)|\\.zip$)"

OUTPUT_DIR="${PWD}/benchmark-results/$(date -u +%Y%m%dT%H%M%SZ)"
AWS_PROFILE_NAME="${AWS_PROFILE:-}"
SUBNET_ID=""
SECURITY_GROUP_ID=""
KEY_NAME=""
SSH_KEY=""
SSH_USER="ubuntu"
HOST=""
INSTANCE_ID=""

KEEP_INSTANCE=0
SKIP_PROVISIONING=0
REUSE_NVME=0
S3_DOWNLOAD_MODE="presign"
PRESIGN_EXPIRES=86400

CREATED_KEY_NAME=""
CREATED_SECURITY_GROUP_ID=""
CREATED_INSTANCE_ID=""

usage() {
  cat <<'USAGE'
Usage:
  scripts/benchmark.sh [options]

Launch an EC2 i4i.2xlarge Ubuntu 24.04 instance, run the README benchmark on
local NVMe storage, copy results back, and terminate the instance by default.

Options:
  --region REGION              EC2 region. Default: eu-west-1
  --profile PROFILE            AWS CLI profile. Default: $AWS_PROFILE if set
  --instance-type TYPE         EC2 instance type. Default: i4i.2xlarge
  --ami-id AMI                 Ubuntu AMI to use. Default: auto-detect Ubuntu 24.04
  --subnet-id ID               Subnet to launch into. Default: first default VPC subnet
  --security-group-id ID       Existing security group with SSH access
  --key-name NAME              Existing EC2 key pair name
  --ssh-key PATH               Private key path for SSH
  --ssh-user USER              SSH user. Default: ubuntu
  --host HOST                  Existing host/IP, required with --skip-provisioning
  --skip-provisioning          Do not create EC2 resources; run on --host instead
  --keep-instance              Do not terminate/delete created EC2 resources
  --reuse-nvme                 Reuse an already mounted NVMe work disk if present
  --output-dir DIR             Local directory for copied benchmark results

Benchmark inputs and versions:
  --input-s3 S3_URI            Input LAS. Default: s3://mfb-data-files/Helsinki_RGB_Class_TM35FIN_EPSG3067.las
  --input-region REGION        S3 object region. Default: eu-west-1
  --input-epsg EPSG            Input CRS EPSG code. Default: 3067
  --gotiler-url URL            Direct gotiler Linux x64 release asset URL
  --gotiler-local-dir DIR      Upload a local gotiler build directory instead of downloading
  --gotiler-repo OWNER/REPO    GitHub repo used if --gotiler-url is omitted
  --gotiler-version TAG        GitHub release tag used if --gotiler-url is omitted
  --py3dtiles-version VERSION  Default: 12.1.1
  --mago-version VERSION       Default: 1.16.4
  --mago-url URL               Direct mago jar URL
  --s3-download-mode MODE      presign, no-sign, or instance-profile. Default: presign
  --presign-expires SECONDS    Presigned input URL lifetime. Default: 86400

Examples:
  scripts/benchmark.sh --gotiler-url https://github.com/.../gotiler-v3.0.0-linux-amd64.zip
  scripts/benchmark.sh --gotiler-local-dir build/linux-amd64
  scripts/benchmark.sh --keep-instance --gotiler-url https://github.com/.../gotiler.zip
  scripts/benchmark.sh --skip-provisioning --host 1.2.3.4 --ssh-key ./bench.pem --reuse-nvme
USAGE
}

log() {
  printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"
}

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "Missing required command: $1"
}

aws_cmd() {
  local region="$1"
  shift
  local args=(--region "$region")
  if [[ -n "$AWS_PROFILE_NAME" ]]; then
    args+=(--profile "$AWS_PROFILE_NAME")
  fi
  aws "${args[@]}" "$@"
}

shell_quote() {
  printf "%q" "$1"
}

select_ssh_tools() {
  SSH_BIN="${SSH_BIN:-ssh}"
  SCP_BIN="${SCP_BIN:-scp}"
  USE_WINDOWS_OPENSSH=0

  case "$SSH_BIN" in
    *Windows/System32/OpenSSH*|*OpenSSH/ssh.exe)
      USE_WINDOWS_OPENSSH=1
      ;;
  esac
}

ssh_local_path() {
  local path="$1"
  if [[ "${USE_WINDOWS_OPENSSH:-0}" -eq 1 ]] && command -v cygpath >/dev/null 2>&1; then
    cygpath -w "$path"
  else
    printf '%s' "$path"
  fi
}

restrict_ssh_key_permissions() {
  local key_path="$1"
  local tmp_path

  if [[ -f "$key_path" ]]; then
    tmp_path="$(mktemp)"
    tr -d '\r' <"$key_path" >"$tmp_path"
    cat "$tmp_path" >"$key_path"
    rm -f "$tmp_path"
  fi

  chmod 600 "$key_path" || true
}

prepare_ssh_key_for_use() {
  local key_path="$1"
  local key_copy

  restrict_ssh_key_permissions "$key_path"
  if ssh-keygen -y -f "$key_path" >/dev/null 2>&1; then
    printf '%s' "$key_path"
    return
  fi

  mkdir -p "$HOME/.ssh"
  chmod 700 "$HOME/.ssh" || true
  key_copy="$HOME/.ssh/$(basename "$key_path")"
  cp "$key_path" "$key_copy"
  restrict_ssh_key_permissions "$key_copy"

  if ssh-keygen -y -f "$key_copy" >/dev/null 2>&1; then
    log "Using chmod-safe SSH key copy at $key_copy" >&2
    printf '%s' "$key_copy"
    return
  fi

  printf '%s' "$key_path"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --region) REGION="$2"; shift 2 ;;
    --profile) AWS_PROFILE_NAME="$2"; shift 2 ;;
    --instance-type) INSTANCE_TYPE="$2"; shift 2 ;;
    --ami-id) AMI_ID="$2"; shift 2 ;;
    --subnet-id) SUBNET_ID="$2"; shift 2 ;;
    --security-group-id) SECURITY_GROUP_ID="$2"; shift 2 ;;
    --key-name) KEY_NAME="$2"; shift 2 ;;
    --ssh-key) SSH_KEY="$2"; shift 2 ;;
    --ssh-user) SSH_USER="$2"; shift 2 ;;
    --host) HOST="$2"; shift 2 ;;
    --skip-provisioning) SKIP_PROVISIONING=1; REUSE_NVME=1; shift ;;
    --keep-instance) KEEP_INSTANCE=1; shift ;;
    --reuse-nvme) REUSE_NVME=1; shift ;;
    --output-dir) OUTPUT_DIR="$2"; shift 2 ;;
    --input-s3) INPUT_S3="$2"; shift 2 ;;
    --input-region) INPUT_REGION="$2"; shift 2 ;;
    --input-epsg) INPUT_EPSG="$2"; shift 2 ;;
    --gotiler-url) GOTILER_URL="$2"; shift 2 ;;
    --gotiler-local-dir) GOTILER_LOCAL_DIR="$2"; shift 2 ;;
    --gotiler-repo) GOTILER_REPO="$2"; shift 2 ;;
    --gotiler-version) GOTILER_VERSION="$2"; shift 2 ;;
    --py3dtiles-version) PY3DTILES_VERSION="$2"; shift 2 ;;
    --mago-version) MAGO_VERSION="$2"; MAGO_URL="https://github.com/Gaia3D/mago-3d-tiler/releases/download/v${2}/mago-3d-tiler-${2}.jar"; shift 2 ;;
    --mago-url) MAGO_URL="$2"; shift 2 ;;
    --s3-download-mode) S3_DOWNLOAD_MODE="$2"; shift 2 ;;
    --presign-expires) PRESIGN_EXPIRES="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "Unknown option: $1" ;;
  esac
done

case "$S3_DOWNLOAD_MODE" in
  presign|no-sign|instance-profile) ;;
  *) die "--s3-download-mode must be presign, no-sign, or instance-profile" ;;
esac

if [[ -n "$GOTILER_LOCAL_DIR" ]]; then
  [[ -d "$GOTILER_LOCAL_DIR" ]] || die "--gotiler-local-dir does not exist or is not a directory: $GOTILER_LOCAL_DIR"
  GOTILER_LOCAL_DIR="$(cd "$GOTILER_LOCAL_DIR" && pwd)"
fi

mkdir -p "$OUTPUT_DIR"

cleanup() {
  local status=$?
  if [[ "$KEEP_INSTANCE" -eq 0 ]]; then
    if [[ -n "$CREATED_INSTANCE_ID" ]]; then
      log "Terminating EC2 instance $CREATED_INSTANCE_ID"
      aws_cmd "$REGION" ec2 terminate-instances --instance-ids "$CREATED_INSTANCE_ID" >/dev/null || true
      aws_cmd "$REGION" ec2 wait instance-terminated --instance-ids "$CREATED_INSTANCE_ID" || true
    fi
    if [[ -n "$CREATED_SECURITY_GROUP_ID" ]]; then
      log "Deleting security group $CREATED_SECURITY_GROUP_ID"
      aws_cmd "$REGION" ec2 delete-security-group --group-id "$CREATED_SECURITY_GROUP_ID" || true
    fi
    if [[ -n "$CREATED_KEY_NAME" ]]; then
      log "Deleting EC2 key pair $CREATED_KEY_NAME"
      aws_cmd "$REGION" ec2 delete-key-pair --key-name "$CREATED_KEY_NAME" || true
    fi
  else
    if [[ -n "$CREATED_INSTANCE_ID" ]]; then
      log "Keeping instance $CREATED_INSTANCE_ID"
    fi
    if [[ -n "$SSH_KEY" && -f "$SSH_KEY" ]]; then
      log "Keeping SSH key at $SSH_KEY"
    fi
  fi
  exit "$status"
}
trap cleanup EXIT

write_remote_runner() {
  local path="$1"
  cat >"$path" <<'REMOTE_RUNNER'
#!/usr/bin/env bash
set -Eeuo pipefail

log() {
  printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"
}

warn() {
  printf '[%s] WARN: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2
}

INPUT_S3="${INPUT_S3:?}"
INPUT_REGION="${INPUT_REGION:?}"
INPUT_EPSG="${INPUT_EPSG:?}"
INPUT_URL="${INPUT_URL:-}"
S3_DOWNLOAD_MODE="${S3_DOWNLOAD_MODE:-presign}"
PY3DTILES_VERSION="${PY3DTILES_VERSION:?}"
MAGO_VERSION="${MAGO_VERSION:?}"
MAGO_URL="${MAGO_URL:?}"
GOTILER_REPO="${GOTILER_REPO:?}"
GOTILER_VERSION="${GOTILER_VERSION:?}"
GOTILER_URL="${GOTILER_URL:-}"
GOTILER_LOCAL_ARCHIVE="${GOTILER_LOCAL_ARCHIVE:-}"
GOTILER_ASSET_REGEX="${GOTILER_ASSET_REGEX:?}"
REUSE_NVME="${REUSE_NVME:-0}"

MOUNT_DIR="/mnt/gotiler-bench"
WORK_DIR="${MOUNT_DIR}/work"
INPUT_DIR="${WORK_DIR}/input"
OUTPUT_ROOT="${WORK_DIR}/outputs"
TEMP_ROOT="${WORK_DIR}/tmp"
RESULTS_DIR="${WORK_DIR}/results"
TOOLS_DIR="${WORK_DIR}/tools"
RESULTS_TAR="/home/ubuntu/gotiler-benchmark-results.tgz"
FAILED_COUNT=0

export DEBIAN_FRONTEND=noninteractive

package_results_on_exit() {
  local status=$?
  if [[ ! -f "$RESULTS_TAR" ]]; then
    mkdir -p "$RESULTS_DIR"
    printf '%s\n' "$status" >"$RESULTS_DIR/remote-exit-code.txt"
    tar -czf "$RESULTS_TAR" -C "$RESULTS_DIR" . 2>/dev/null || true
    chown ubuntu:ubuntu "$RESULTS_TAR" 2>/dev/null || true
  fi
}
trap package_results_on_exit EXIT

install_packages() {
  log "Installing system dependencies"
  apt-get update
  apt-get install -y \
    bc \
    build-essential \
    ca-certificates \
    curl \
    file \
    jq \
    mdadm \
    nvme-cli \
    openjdk-21-jre-headless \
    python3-dev \
    python3-pip \
    python3-venv \
    time \
    unzip \
    xfsprogs
}

install_aws_cli_v2() {
  if command -v aws >/dev/null 2>&1; then
    return
  fi

  log "Installing AWS CLI v2"
  local arch url tmp_dir
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) url="https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" ;;
    aarch64|arm64) url="https://awscli.amazonaws.com/awscli-exe-linux-aarch64.zip" ;;
    *) echo "Unsupported architecture for AWS CLI v2: $arch" >&2; exit 1 ;;
  esac

  tmp_dir="$(mktemp -d)"
  curl -fL --retry 5 --retry-delay 3 "$url" -o "$tmp_dir/awscliv2.zip"
  unzip -q "$tmp_dir/awscliv2.zip" -d "$tmp_dir"
  "$tmp_dir/aws/install" --update
  rm -rf "$tmp_dir"
}

root_disk() {
  local root_source pk
  root_source="$(findmnt -n -o SOURCE /)"
  pk="$(lsblk -no PKNAME "$root_source" 2>/dev/null | head -n1 || true)"
  if [[ -n "$pk" ]]; then
    readlink -f "/dev/$pk"
  else
    readlink -f "$root_source"
  fi
}

find_instance_store_devices() {
  local root candidate
  root="$(root_disk)"

  if compgen -G "/dev/disk/by-id/nvme-Amazon_EC2_NVMe_Instance_Storage*" >/dev/null; then
    for candidate in /dev/disk/by-id/nvme-Amazon_EC2_NVMe_Instance_Storage*; do
      readlink -f "$candidate"
    done
    return
  fi

  lsblk -dpno NAME,TYPE,MODEL | while read -r name type model; do
    [[ "$type" == "disk" ]] || continue
    [[ "$(readlink -f "$name")" != "$root" ]] || continue
    if [[ "$model" == *"Instance Storage"* || "$model" != *"Elastic Block Store"* ]]; then
      printf '%s\n' "$name"
    fi
  done
}

mount_fast_storage() {
  log "Preparing fast NVMe workspace"
  mkdir -p "$MOUNT_DIR"

  if mountpoint -q "$MOUNT_DIR"; then
    log "$MOUNT_DIR is already mounted"
    return
  fi

  mapfile -t devices < <(find_instance_store_devices | sort -u)
  if [[ "${#devices[@]}" -eq 0 ]]; then
    warn "No EC2 instance-store NVMe disk found; using root EBS at $MOUNT_DIR"
    return
  fi

  if [[ "$REUSE_NVME" == "1" ]]; then
    for dev in "${devices[@]}"; do
      if blkid "$dev" >/dev/null 2>&1; then
        mount -o noatime "$dev" "$MOUNT_DIR" && return
      fi
    done
  fi

  if [[ "${#devices[@]}" -eq 1 ]]; then
    local dev="${devices[0]}"
    log "Formatting and mounting $dev"
    wipefs -a "$dev" || true
    mkfs.xfs -f "$dev"
    mount -o noatime "$dev" "$MOUNT_DIR"
  else
    log "Creating RAID0 over ${#devices[@]} NVMe devices"
    mdadm --create /dev/md0 --level=0 --raid-devices="${#devices[@]}" "${devices[@]}"
    mkfs.xfs -f /dev/md0
    mount -o noatime /dev/md0 "$MOUNT_DIR"
  fi
}

prepare_workspace() {
  mkdir -p "$INPUT_DIR" "$OUTPUT_ROOT" "$TEMP_ROOT" "$RESULTS_DIR/tool-help" "$TOOLS_DIR"
  chmod 0777 "$TEMP_ROOT"
  export TMPDIR="$TEMP_ROOT"
  rm -rf "$OUTPUT_ROOT"/*
}

resolve_github_release_asset() {
  python3 - <<'PY'
import json
import os
import re
import sys
import urllib.request

repo = os.environ["GOTILER_REPO"]
tag = os.environ["GOTILER_VERSION"]
pattern = re.compile(os.environ["GOTILER_ASSET_REGEX"], re.I)

url = f"https://api.github.com/repos/{repo}/releases/tags/{tag}"
req = urllib.request.Request(url, headers={"User-Agent": "gotiler-benchmark"})
try:
    with urllib.request.urlopen(req, timeout=30) as response:
        release = json.load(response)
except Exception as exc:
    print(f"Could not fetch release {repo}@{tag}: {exc}", file=sys.stderr)
    sys.exit(1)

matches = [
    asset["browser_download_url"]
    for asset in release.get("assets", [])
    if pattern.search(asset.get("name", ""))
]
if not matches:
    names = ", ".join(asset.get("name", "") for asset in release.get("assets", []))
    print(f"No gotiler asset matched {pattern.pattern!r}. Assets: {names}", file=sys.stderr)
    sys.exit(2)

print(matches[0])
PY
}

install_gotiler() {
  log "Installing gotiler ${GOTILER_VERSION}"
  mkdir -p "$TOOLS_DIR/gotiler"

  if [[ -n "$GOTILER_LOCAL_ARCHIVE" ]]; then
    log "Using uploaded local gotiler build: $GOTILER_LOCAL_ARCHIVE"
    mkdir -p "$TOOLS_DIR/gotiler/extract"
    tar -xzf "$GOTILER_LOCAL_ARCHIVE" -C "$TOOLS_DIR/gotiler/extract"
  else
    local url="${GOTILER_URL}"
    if [[ -z "$url" ]]; then
      url="$(resolve_github_release_asset)"
    fi

    local artifact="$TOOLS_DIR/gotiler/artifact"
    curl -fL --retry 5 --retry-delay 3 "$url" -o "$artifact"

    if file "$artifact" | grep -qi 'zip archive'; then
      unzip -q "$artifact" -d "$TOOLS_DIR/gotiler/extract"
    elif file "$artifact" | grep -Eqi 'gzip compressed|tar archive'; then
      mkdir -p "$TOOLS_DIR/gotiler/extract"
      tar -xf "$artifact" -C "$TOOLS_DIR/gotiler/extract"
    else
      mkdir -p "$TOOLS_DIR/gotiler/extract"
      cp "$artifact" "$TOOLS_DIR/gotiler/extract/gotiler"
    fi
  fi

  chmod -R a+rx "$TOOLS_DIR/gotiler/extract"
  GOTILER_BIN="$(
    find "$TOOLS_DIR/gotiler/extract" -type f \
      \( -iname '*lin*x64*' -o -iname '*linux*amd64*' -o -iname '*linux*x86_64*' \) \
      ! -iname '*.exe' ! -iname '*.zip' ! -iname '*.md' \
      -print | sort | head -n1
  )"
  if [[ -z "$GOTILER_BIN" ]]; then
    GOTILER_BIN="$(
    find "$TOOLS_DIR/gotiler/extract" -type f \
      \( -iname 'gotiler' -o -iname 'gotiler-*' -o -iname 'gocesiumtiler' -o -iname 'gocesiumtiler-*' \) \
      ! -iname '*.exe' ! -iname '*.zip' ! -iname '*.md' \
      -print | sort | head -n1
    )"
  fi
  [[ -n "$GOTILER_BIN" ]] || {
    find "$TOOLS_DIR/gotiler/extract" -maxdepth 3 -type f -ls >&2
    echo "Could not find gotiler Linux executable in release asset" >&2
    exit 1
  }
  "$GOTILER_BIN" version >"$RESULTS_DIR/tool-help/gotiler-version.txt" 2>&1 || true
}

install_py3dtiles() {
  log "Installing py3dtiles ${PY3DTILES_VERSION}"
  python3 -m venv "$TOOLS_DIR/py3dtiles-venv"
  "$TOOLS_DIR/py3dtiles-venv/bin/python" -m pip install --upgrade pip
  "$TOOLS_DIR/py3dtiles-venv/bin/python" -m pip install "py3dtiles[las]==${PY3DTILES_VERSION}" matplotlib
  PY3DTILES_BIN="$TOOLS_DIR/py3dtiles-venv/bin/py3dtiles"
  "$PY3DTILES_BIN" convert --help >"$RESULTS_DIR/tool-help/py3dtiles-convert-help.txt" 2>&1
}

install_mago() {
  log "Installing mago 3d tiler ${MAGO_VERSION}"
  mkdir -p "$TOOLS_DIR/mago"
  MAGO_JAR="$TOOLS_DIR/mago/mago-3d-tiler-${MAGO_VERSION}.jar"
  curl -fL --retry 5 --retry-delay 3 "$MAGO_URL" -o "$MAGO_JAR"
  java -jar "$MAGO_JAR" --help >"$RESULTS_DIR/tool-help/mago-help.txt" 2>&1 || true
}

download_input() {
  local basename
  basename="$(basename "$INPUT_S3")"
  INPUT_FILE="$INPUT_DIR/$basename"

  if [[ -s "$INPUT_FILE" ]]; then
    log "Reusing existing input file $INPUT_FILE"
    return
  fi

  log "Downloading input dataset to NVMe workspace"
  if [[ "$S3_DOWNLOAD_MODE" == "presign" && -n "$INPUT_URL" ]]; then
    curl -fL --retry 5 --retry-delay 10 -o "$INPUT_FILE" "$INPUT_URL"
  elif [[ "$S3_DOWNLOAD_MODE" == "no-sign" ]]; then
    install_aws_cli_v2
    aws s3 cp --region "$INPUT_REGION" --no-sign-request "$INPUT_S3" "$INPUT_FILE"
  else
    install_aws_cli_v2
    aws s3 cp --region "$INPUT_REGION" "$INPUT_S3" "$INPUT_FILE"
  fi
}

elapsed_to_seconds() {
  python3 - "$1" <<'PY'
import sys

value = sys.argv[1].strip()
days = 0
if "-" in value:
    left, value = value.split("-", 1)
    days = int(left)
parts = value.split(":")
if len(parts) == 3:
    hours, minutes, seconds = parts
elif len(parts) == 2:
    hours = 0
    minutes, seconds = parts
else:
    hours = 0
    minutes = 0
    seconds = parts[0]
total = days * 86400 + int(hours) * 3600 + int(minutes) * 60 + float(seconds)
print(f"{total:.3f}")
PY
}

csv_escape() {
  local value="${1//\"/\"\"}"
  printf '"%s"' "$value"
}

record_result() {
  local tool="$1"
  local version="$2"
  local exit_code="$3"
  local elapsed="$4"
  local rss_kb="$5"
  local output_bytes="$6"
  local command="$7"
  local seconds rss_mb
  seconds="$(elapsed_to_seconds "$elapsed")"
  rss_mb="$(awk -v kb="$rss_kb" 'BEGIN { printf "%.2f", kb / 1024 }')"

  {
    csv_escape "$tool"; printf ','
    csv_escape "$version"; printf ','
    csv_escape "$exit_code"; printf ','
    csv_escape "$seconds"; printf ','
    csv_escape "$rss_mb"; printf ','
    csv_escape "$output_bytes"; printf ','
    csv_escape "$command"; printf '\n'
  } >>"$RESULTS_DIR/results.csv"
}

drop_caches() {
  sync || true
  echo 3 >/proc/sys/vm/drop_caches || true
}

run_benchmark() {
  local name="$1"
  local version="$2"
  shift 2

  local outdir="$OUTPUT_ROOT/$name"
  local logdir="$RESULTS_DIR/logs/$name"
  local timefile="$logdir/time.txt"
  local stdout="$logdir/stdout.txt"
  local stderr="$logdir/stderr.txt"
  local command_text

  mkdir -p "$outdir" "$logdir"
  rm -rf "$outdir"/*
  command_text="$(printf '%q ' "$@")"
  printf '%s\n' "$command_text" >"$logdir/command.txt"

  log "Running $name"
  drop_caches

  set +e
  /usr/bin/time -v -o "$timefile" "$@" >"$stdout" 2>"$stderr"
  local exit_code=$?
  set -e

  local elapsed rss_kb output_bytes
  elapsed="$(awk -F': ' '/Elapsed \(wall clock\)/ {print $2}' "$timefile" | tail -n1)"
  rss_kb="$(awk -F': ' '/Maximum resident set size/ {print $2}' "$timefile" | tail -n1)"
  output_bytes="$(du -sb "$outdir" 2>/dev/null | awk '{print $1}')"
  elapsed="${elapsed:-0:00.00}"
  rss_kb="${rss_kb:-0}"
  output_bytes="${output_bytes:-0}"

  record_result "$name" "$version" "$exit_code" "$elapsed" "$rss_kb" "$output_bytes" "$command_text"

  if [[ "$exit_code" -ne 0 ]]; then
    warn "$name failed with exit code $exit_code"
    FAILED_COUNT=$((FAILED_COUNT + 1))
  fi
}

generate_reports() {
  log "Generating benchmark reports"
  "$TOOLS_DIR/py3dtiles-venv/bin/python" - "$RESULTS_DIR/results.csv" "$RESULTS_DIR" <<'PY'
import csv
import pathlib
import sys

import matplotlib.pyplot as plt

csv_path = pathlib.Path(sys.argv[1])
results_dir = pathlib.Path(sys.argv[2])

rows = list(csv.DictReader(csv_path.open(newline="")))

def fmt_seconds(value: str) -> str:
    seconds = float(value)
    minutes = int(seconds // 60)
    rest = seconds - minutes * 60
    if minutes:
        return f"{minutes}m {rest:.1f}s"
    return f"{rest:.1f}s"

md = [
    "# GoTiler Benchmark Results",
    "",
    f"Input: `{csv_path}`",
    "",
    "| Tool | Version | Exit | Time | Max RSS | Output size |",
    "|---|---:|---:|---:|---:|---:|",
]
for row in rows:
    out_gb = int(row["output_bytes"]) / (1024 ** 3)
    md.append(
        f"| {row['tool']} | {row['version']} | {row['exit_code']} | "
        f"{fmt_seconds(row['elapsed_seconds'])} | {float(row['max_rss_mb']):.2f} MB | {out_gb:.2f} GB |"
    )
md.append("")
md.append("Commands are recorded under `logs/<tool>/command.txt`; raw GNU time output is in `logs/<tool>/time.txt`.")
(results_dir / "summary.md").write_text("\n".join(md), encoding="utf-8")

labels = [row["tool"] for row in rows]

def chart(filename: str, key: str, ylabel: str, title: str) -> None:
    values = [float(row[key]) for row in rows]
    fig, ax = plt.subplots(figsize=(8, 4.5))
    bars = ax.bar(labels, values, color=["#2e7d32", "#1565c0", "#6a1b9a"][: len(labels)])
    ax.set_title(title)
    ax.set_ylabel(ylabel)
    ax.grid(axis="y", linestyle="--", alpha=0.35)
    for bar, value in zip(bars, values):
        ax.text(
            bar.get_x() + bar.get_width() / 2,
            bar.get_height(),
            f"{value:.1f}",
            ha="center",
            va="bottom",
            fontsize=9,
        )
    fig.tight_layout()
    fig.savefig(results_dir / filename, dpi=160)
    plt.close(fig)

chart("execution-time-seconds.png", "elapsed_seconds", "Seconds", "Execution Time")
chart("max-memory-mb.png", "max_rss_mb", "MB", "Maximum Resident Memory")
PY
}

main() {
  install_packages
  mount_fast_storage
  prepare_workspace

  {
    printf 'tool,version,exit_code,elapsed_seconds,max_rss_mb,output_bytes,command\n'
  } >"$RESULTS_DIR/results.csv"

  install_gotiler
  install_py3dtiles
  install_mago
  download_input

  run_benchmark \
    "gotiler" \
    "$GOTILER_VERSION" \
    "$GOTILER_BIN" \
    --out "$OUTPUT_ROOT/gotiler" \
    --crs "EPSG:${INPUT_EPSG}" \
    --version "1.0" \
    --attributes "none" \
    "$INPUT_FILE"

  run_benchmark \
    "py3dtiles" \
    "$PY3DTILES_VERSION" \
    "$PY3DTILES_BIN" \
    convert "$INPUT_FILE" \
    --out "$OUTPUT_ROOT/py3dtiles" \
    --overwrite \
    --srs_in "$INPUT_EPSG" \
    --srs_out "4978" \
    --spec-version "1.0"

  run_benchmark \
    "mago-3d-tiler" \
    "$MAGO_VERSION" \
    java -jar "$MAGO_JAR" \
    --input "$INPUT_DIR" \
    --output "$OUTPUT_ROOT/mago-3d-tiler" \
    --temp "$TEMP_ROOT/mago" \
    --inputType "las" \
    --outputType "pnts" \
    --tilesVersion "1.0" \
    --crs "$INPUT_EPSG"

  generate_reports
  tar -czf "$RESULTS_TAR" -C "$RESULTS_DIR" .
  chown ubuntu:ubuntu "$RESULTS_TAR" || true

  if [[ "$FAILED_COUNT" -gt 0 ]]; then
    warn "$FAILED_COUNT benchmark command(s) failed"
    exit 1
  fi
}

main "$@"
REMOTE_RUNNER
}

launch_instance() {
  require_cmd aws
  require_cmd curl

  local ami_id
  ami_id="$(resolve_ami_id)"

  if [[ -z "$SUBNET_ID" ]]; then
    local vpc_id
    vpc_id="$(aws_cmd "$REGION" ec2 describe-vpcs --filters Name=isDefault,Values=true --query 'Vpcs[0].VpcId' --output text)"
    [[ -n "$vpc_id" && "$vpc_id" != "None" ]] || die "No default VPC found. Pass --subnet-id and --security-group-id."
    SUBNET_ID="$(
      aws_cmd "$REGION" ec2 describe-subnets \
        --filters "Name=vpc-id,Values=${vpc_id}" "Name=default-for-az,Values=true" \
        --query 'Subnets[0].SubnetId' \
        --output text
    )"
    [[ -n "$SUBNET_ID" && "$SUBNET_ID" != "None" ]] || die "No default subnet found. Pass --subnet-id."
  fi

  if [[ -z "$SECURITY_GROUP_ID" ]]; then
    local vpc_id group_name caller_ip
    vpc_id="$(aws_cmd "$REGION" ec2 describe-subnets --subnet-ids "$SUBNET_ID" --query 'Subnets[0].VpcId' --output text)"
    group_name="gotiler-benchmark-$(date -u +%Y%m%d%H%M%S)"
    caller_ip="$(curl -fsS https://checkip.amazonaws.com | tr -d '[:space:]')"
    [[ -n "$caller_ip" ]] || die "Could not determine public IP for temporary SSH security group"
    SECURITY_GROUP_ID="$(
      aws_cmd "$REGION" ec2 create-security-group \
        --group-name "$group_name" \
        --description "Temporary SSH access for gotiler benchmark" \
        --vpc-id "$vpc_id" \
        --query 'GroupId' \
        --output text
    )"
    CREATED_SECURITY_GROUP_ID="$SECURITY_GROUP_ID"
    aws_cmd "$REGION" ec2 authorize-security-group-ingress \
      --group-id "$SECURITY_GROUP_ID" \
      --ip-permissions "IpProtocol=tcp,FromPort=22,ToPort=22,IpRanges=[{CidrIp=${caller_ip}/32,Description='gotiler benchmark ssh'}]" \
      >/dev/null
  fi

  if [[ -z "$KEY_NAME" ]]; then
    KEY_NAME="gotiler-benchmark-$(date -u +%Y%m%d%H%M%S)"
    SSH_KEY="${OUTPUT_DIR}/${KEY_NAME}.pem"
    aws_cmd "$REGION" ec2 create-key-pair \
      --key-name "$KEY_NAME" \
      --key-type ed25519 \
      --query 'KeyMaterial' \
      --output text >"$SSH_KEY"
    restrict_ssh_key_permissions "$SSH_KEY"
    CREATED_KEY_NAME="$KEY_NAME"
  fi

  [[ -n "$SSH_KEY" && -f "$SSH_KEY" ]] || die "Pass --ssh-key for key pair $KEY_NAME"

  log "Launching $INSTANCE_TYPE in $REGION"
  INSTANCE_ID="$(
    aws_cmd "$REGION" ec2 run-instances \
      --image-id "$ami_id" \
      --instance-type "$INSTANCE_TYPE" \
      --key-name "$KEY_NAME" \
      --security-group-ids "$SECURITY_GROUP_ID" \
      --subnet-id "$SUBNET_ID" \
      --associate-public-ip-address \
      --instance-initiated-shutdown-behavior terminate \
      --metadata-options 'HttpEndpoint=enabled,HttpTokens=required' \
      --block-device-mappings '[{"DeviceName":"/dev/sda1","Ebs":{"VolumeSize":80,"VolumeType":"gp3","DeleteOnTermination":true}}]' \
      --tag-specifications 'ResourceType=instance,Tags=[{Key=Name,Value=gotiler-readme-benchmark},{Key=Project,Value=gotiler},{Key=Purpose,Value=benchmark}]' \
      --query 'Instances[0].InstanceId' \
      --output text
  )"
  CREATED_INSTANCE_ID="$INSTANCE_ID"

  aws_cmd "$REGION" ec2 wait instance-running --instance-ids "$INSTANCE_ID"
  aws_cmd "$REGION" ec2 wait instance-status-ok --instance-ids "$INSTANCE_ID"
  HOST="$(aws_cmd "$REGION" ec2 describe-instances --instance-ids "$INSTANCE_ID" --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)"
  [[ -n "$HOST" && "$HOST" != "None" ]] || die "Launched instance has no public IP"
  log "Instance $INSTANCE_ID is reachable at $HOST"
}

build_input_url() {
  if [[ "$S3_DOWNLOAD_MODE" != "presign" ]]; then
    printf ''
    return
  fi

  local url
  if ! url="$(aws_cmd "$INPUT_REGION" s3 presign "$INPUT_S3" --expires-in "$PRESIGN_EXPIRES" 2>/dev/null)"; then
    log "Could not presign $INPUT_S3; remote side will try AWS/S3 access directly"
    printf ''
    return
  fi
  printf '%s' "$url"
}

wait_for_ssh() {
  local ssh_base=("$@")
  local ssh_output=""
  log "Waiting for SSH"
  for _ in $(seq 1 90); do
    if ssh_output="$("${ssh_base[@]}" "true" 2>&1)"; then
      return
    fi
    if [[ "$ssh_output" == *"UNPROTECTED PRIVATE KEY FILE"* || "$ssh_output" == *"bad permissions"* || "$ssh_output" == *"Permission denied (publickey)"* ]]; then
      printf '%s\n' "$ssh_output" >&2
      die "SSH authentication failed. Check --ssh-key permissions and key pair name."
    fi
    sleep 5
  done
  die "SSH did not become ready"
}

run_remote_benchmark() {
  select_ssh_tools
  require_cmd "$SSH_BIN"
  require_cmd "$SCP_BIN"
  require_cmd ssh-keygen
  log "Using SSH client: $SSH_BIN"

  local remote_script="${OUTPUT_DIR}/remote-benchmark-runner.sh"
  local known_hosts="${OUTPUT_DIR}/known_hosts"
  local input_url
  local local_gotiler_archive=""
  local remote_gotiler_archive=""
  local ssh_key_arg=""
  local known_hosts_arg
  write_remote_runner "$remote_script"
  input_url="$(build_input_url)"

  known_hosts_arg="$(ssh_local_path "$known_hosts")"
  local ssh_opts=(-o StrictHostKeyChecking=no -o UserKnownHostsFile="$known_hosts_arg" -o ServerAliveInterval=30 -o ServerAliveCountMax=20)
  if [[ -n "$SSH_KEY" ]]; then
    SSH_KEY="$(prepare_ssh_key_for_use "$SSH_KEY")"
    ssh_key_arg="$(ssh_local_path "$SSH_KEY")"
    ssh_opts+=(-i "$ssh_key_arg")
  fi
  local dest="${SSH_USER}@${HOST}"
  local ssh_base=("$SSH_BIN" "${ssh_opts[@]}" "$dest")
  local scp_base=("$SCP_BIN" "${ssh_opts[@]}")

  wait_for_ssh "${ssh_base[@]}"

  log "Copying remote benchmark runner"
  "${scp_base[@]}" "$(ssh_local_path "$remote_script")" "$dest:/home/${SSH_USER}/gotiler-benchmark-runner.sh"

  if [[ -n "$GOTILER_LOCAL_DIR" ]]; then
    local_gotiler_archive="${OUTPUT_DIR}/gotiler-local-amd64.tgz"
    remote_gotiler_archive="/home/${SSH_USER}/gotiler-local-amd64.tgz"
    log "Packaging local gotiler build from $GOTILER_LOCAL_DIR"
    tar -czf "$local_gotiler_archive" -C "$GOTILER_LOCAL_DIR" .
    log "Uploading local gotiler build"
    "${scp_base[@]}" "$(ssh_local_path "$local_gotiler_archive")" "$dest:$remote_gotiler_archive"
  fi

  local env_parts=(
    "INPUT_S3=$(shell_quote "$INPUT_S3")"
    "INPUT_REGION=$(shell_quote "$INPUT_REGION")"
    "INPUT_EPSG=$(shell_quote "$INPUT_EPSG")"
    "INPUT_URL=$(shell_quote "$input_url")"
    "S3_DOWNLOAD_MODE=$(shell_quote "$S3_DOWNLOAD_MODE")"
    "PY3DTILES_VERSION=$(shell_quote "$PY3DTILES_VERSION")"
    "MAGO_VERSION=$(shell_quote "$MAGO_VERSION")"
    "MAGO_URL=$(shell_quote "$MAGO_URL")"
    "GOTILER_REPO=$(shell_quote "$GOTILER_REPO")"
    "GOTILER_VERSION=$(shell_quote "$GOTILER_VERSION")"
    "GOTILER_URL=$(shell_quote "$GOTILER_URL")"
    "GOTILER_LOCAL_ARCHIVE=$(shell_quote "$remote_gotiler_archive")"
    "GOTILER_ASSET_REGEX=$(shell_quote "$GOTILER_ASSET_REGEX")"
    "REUSE_NVME=$(shell_quote "$REUSE_NVME")"
  )

  log "Running remote benchmark"
  set +e
  "${ssh_base[@]}" "chmod +x /home/${SSH_USER}/gotiler-benchmark-runner.sh && sudo env ${env_parts[*]} /home/${SSH_USER}/gotiler-benchmark-runner.sh"
  local remote_status=$?
  set -e

  log "Copying benchmark results back to $OUTPUT_DIR"
  "${scp_base[@]}" "$dest:/home/${SSH_USER}/gotiler-benchmark-results.tgz" "$(ssh_local_path "${OUTPUT_DIR}/gotiler-benchmark-results.tgz")" || true
  if [[ -f "${OUTPUT_DIR}/gotiler-benchmark-results.tgz" ]]; then
    mkdir -p "${OUTPUT_DIR}/results"
    tar -xzf "${OUTPUT_DIR}/gotiler-benchmark-results.tgz" -C "${OUTPUT_DIR}/results"
  fi

  return "$remote_status"
}

resolve_ami_id() {
  if [[ -n "$AMI_ID" ]]; then
    printf '%s\n' "$AMI_ID"
    return
  fi

  local ami_id=""
  log "Resolving Ubuntu 24.04 AMI" >&2

  if ami_id="$(aws_cmd "$REGION" ssm get-parameter --name "$AMI_SSM_PARAM" --query 'Parameter.Value' --output text 2>/dev/null)" \
    && [[ -n "$ami_id" && "$ami_id" != "None" ]]; then
    printf '%s\n' "$ami_id"
    return
  fi

  log "SSM parameter lookup failed; searching Canonical public AMIs" >&2
  local pattern
  for pattern in \
    "ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*" \
    "ubuntu/images/hvm-ssd/ubuntu-noble-24.04-amd64-server-*"; do
    ami_id="$(
      aws_cmd "$REGION" ec2 describe-images \
        --owners 099720109477 \
        --filters \
          "Name=name,Values=${pattern}" \
          "Name=state,Values=available" \
          "Name=architecture,Values=x86_64" \
          "Name=virtualization-type,Values=hvm" \
          "Name=root-device-type,Values=ebs" \
        --query 'sort_by(Images,&CreationDate)[-1].ImageId' \
        --output text 2>/dev/null || true
    )"
    if [[ -n "$ami_id" && "$ami_id" != "None" ]]; then
      printf '%s\n' "$ami_id"
      return
    fi
  done

  die "Could not resolve an Ubuntu 24.04 amd64 AMI in $REGION. Pass --ami-id explicitly."
}

main() {
  require_cmd tar

  if [[ "$SKIP_PROVISIONING" -eq 0 ]]; then
    launch_instance
  else
    [[ -n "$HOST" ]] || die "--host is required with --skip-provisioning"
  fi

  run_remote_benchmark
}

main
