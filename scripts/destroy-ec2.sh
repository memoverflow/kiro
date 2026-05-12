#!/usr/bin/env bash
# destroy-ec2.sh — 销毁所有 kiro-proxy 相关资源
set -euo pipefail

[[ -f ./.kiro-proxy.env ]] || { echo "未找到 .kiro-proxy.env" >&2; exit 1; }
source ./.kiro-proxy.env

read -rp "销毁 EC2 实例 $INSTANCE_ID + EIP + SG $SG_ID ? (yes/no) " ans
[[ "$ans" == "yes" ]] || { echo "取消"; exit 0; }

echo "▸ 停止实例..."
aws ec2 terminate-instances --region "$REGION" --instance-ids "$INSTANCE_ID" >/dev/null
aws ec2 wait instance-terminated --region "$REGION" --instance-ids "$INSTANCE_ID"

echo "▸ 释放 EIP..."
aws ec2 release-address --region "$REGION" --allocation-id "$ALLOC_ID" || true

echo "▸ 删除 SG..."
aws ec2 delete-security-group --region "$REGION" --group-id "$SG_ID" || true

rm -f ./.kiro-proxy.env
echo "✓ 已清理"
