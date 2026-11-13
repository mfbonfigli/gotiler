#!/usr/bin/env bash

# Deploys the live demo viewer in demo/ to its S3 bucket, then clears the
# CloudFront cache of the uploaded files. The tilesets the viewer shows sit
# next to it in the bucket and are uploaded separately.
#
# Usage: scripts/deploy-demo.sh [--dry-run] beta|live
#   beta  uploads to beta/, to test at <site>/beta/index.html
#   live  uploads to the bucket root, which the docs' Live demo page shows.
#         The first live deploy keeps the page it replaces as index-old.html.
#   --dry-run lists the uploads without making them.
#
# BUCKET, DISTRIBUTION_ID and AWS_REGION override the defaults below. The AWS
# CLI must be signed in to the account that owns the bucket.

set -o errexit
set -o nounset
set -o pipefail

bucket="${BUCKET:-gocesiumtiler-demo}"
distribution_id="${DISTRIBUTION_ID:-E2RY4M0EE098O3}"
region="${AWS_REGION:-eu-west-1}"
site="https://d39maarsub1d2t.cloudfront.net"

# Git Bash would rewrite arguments such as /index.html into Windows paths
# before the AWS CLI sees them
export MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*'

usage() {
    echo "Usage: $0 [--dry-run] beta|live" >&2
    exit 1
}

dry_run=()
target=""
for arg in "$@"; do
    case "$arg" in
        --dry-run) dry_run=(--dryrun) ;;
        beta | live)
            [[ -z "$target" ]] || usage
            target="$arg"
            ;;
        *) usage ;;
    esac
done
[[ -n "$target" ]] || usage

if ! command -v aws >/dev/null 2>&1; then
    echo "The AWS CLI (aws) is not installed." >&2
    exit 1
fi

cd "$(dirname "${BASH_SOURCE[0]}")/.."
prefix=""
[[ "$target" == beta ]] && prefix="beta/"
destination="s3://$bucket/$prefix"

mapfile -t files < <(cd demo && find . -maxdepth 1 -type f \( -name '*.html' -o -name '*.js' -o -name '*.css' \) | sed 's|^\./||' | sort)
if [[ ${#files[@]} -eq 0 ]]; then
    echo "demo/ holds no page to upload." >&2
    exit 1
fi

# The page the first live deploy replaces is not in the repository: keep it.
# Later versions of the viewer are in the repository's history.
if [[ "$target" == live ]] && ! aws s3api head-object --bucket "$bucket" --key index-old.html --region "$region" >/dev/null 2>&1; then
    echo "==> Keeping the current index.html as index-old.html"
    aws s3 cp "s3://$bucket/index.html" "s3://$bucket/index-old.html" --region "$region" ${dry_run[@]+"${dry_run[@]}"}
fi

# Every file is uploaded each time, as the page is small: sync would skip a
# file whose copy in the bucket is newer, as after checking out an older
# version. One upload per file type, each with its content type: module
# scripts fail to load when a machine guesses theirs wrong. no-cache makes
# browsers check for a new version on every visit, so a new page never runs
# old cached scripts. Nothing is deleted: the bucket root also holds the
# tilesets.
echo "==> Uploading demo/ to $destination"
for type in 'html:text/html' 'js:text/javascript' 'css:text/css'; do
    aws s3 cp demo/ "$destination" --recursive --region "$region" ${dry_run[@]+"${dry_run[@]}"} \
        --exclude '*' --include "*.${type%%:*}" \
        --content-type "${type#*:}; charset=utf-8" --cache-control no-cache
done

if [[ "$target" == beta ]]; then
    paths=("/beta/*")
else
    paths=("/")
    for file in "${files[@]}"; do
        paths+=("/$file")
    done
fi
if [[ ${#dry_run[@]} -gt 0 ]]; then
    echo "==> Would clear the CloudFront cache of: ${paths[*]}"
else
    echo "==> Clearing the CloudFront cache of: ${paths[*]}"
    aws cloudfront create-invalidation --distribution-id "$distribution_id" --paths "${paths[@]}" \
        --query 'Invalidation.[Id,Status]' --output text
fi

echo "==> Done: $site/${prefix}index.html"
