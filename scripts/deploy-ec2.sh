#!/usr/bin/env bash
# deploy-ec2.sh — 在 us-east-1 开一台 EC2 跑 sing-box Shadowsocks 2022，
# 严格 /32 本机 IP 白名单。
#
# 前置：aws-cli 已 configure，有 EC2/VPC 权限；jq 已装。
#
# 产出：./.kiro-proxy.env  (EC2 endpoint + SS 密钥 + 运维 ID)

set -euo pipefail
cd "$(dirname "$0")/.."

REGION="us-east-1"
INSTANCE_TYPE="t4g.nano"
KEY_NAME="kiro-proxy-key"
SG_NAME="kiro-proxy-sg"
INSTANCE_NAME="kiro-proxy"
SS_PORT=1443
SS_METHOD="2022-blake3-aes-128-gcm"
SS_PASS="$(openssl rand -base64 16)"

log() { printf "\033[1;36m▸\033[0m %s\n" "$*"; }
err() { printf "\033[1;31m✗\033[0m %s\n" "$*" >&2; exit 1; }

command -v aws >/dev/null || err "aws-cli 未安装"
command -v jq  >/dev/null || err "jq 未安装 (brew install jq)"
aws sts get-caller-identity >/dev/null 2>&1 || err "aws 未登录"

MY_IP="$(curl -s https://checkip.amazonaws.com | tr -d '\n')"
[[ "$MY_IP" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] || err "取本机公网 IP 失败"
log "本机公网 IP: $MY_IP (SG 只开 /32)"

if [[ "${ALLOW_ANY_CIDR:-}" == "yes" ]]; then
  err "SS 明文暴露全网会被扫端口滥用，此开关不接受"
fi

# ─── AMI ─────────────────────────────────────────────────────────
log "查最新 Ubuntu 24.04 ARM AMI..."
AMI_ID="$(aws ec2 describe-images --region "$REGION" \
  --owners 099720109477 \
  --filters \
    "Name=name,Values=ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-arm64-server-*" \
    "Name=state,Values=available" \
  --query 'Images | sort_by(@, &CreationDate) | [-1].ImageId' --output text)"
[[ "$AMI_ID" == "None" || -z "$AMI_ID" ]] && err "找不到 AMI"

# ─── SSH key ─────────────────────────────────────────────────────
SSH_KEY="$HOME/.ssh/${KEY_NAME}.pem"
if ! aws ec2 describe-key-pairs --region "$REGION" --key-names "$KEY_NAME" >/dev/null 2>&1; then
  log "创建 key pair"
  aws ec2 create-key-pair --region "$REGION" --key-name "$KEY_NAME" \
    --query 'KeyMaterial' --output text > "$SSH_KEY"
  chmod 400 "$SSH_KEY"
fi

# ─── SG ──────────────────────────────────────────────────────────
VPC_ID="$(aws ec2 describe-vpcs --region "$REGION" \
  --filters "Name=is-default,Values=true" \
  --query 'Vpcs[0].VpcId' --output text)"

SG_ID="$(aws ec2 describe-security-groups --region "$REGION" \
  --filters "Name=group-name,Values=$SG_NAME" "Name=vpc-id,Values=$VPC_ID" \
  --query 'SecurityGroups[0].GroupId' --output text 2>/dev/null || echo None)"

if [[ "$SG_ID" == "None" || -z "$SG_ID" ]]; then
  log "创建 SG"
  SG_ID="$(aws ec2 create-security-group --region "$REGION" \
    --group-name "$SG_NAME" --description "kiro-proxy (strict /32)" \
    --vpc-id "$VPC_ID" --query 'GroupId' --output text)"
fi

log "SG 刷新入站规则 ${MY_IP}/32"
aws ec2 revoke-security-group-ingress --region "$REGION" --group-id "$SG_ID" \
  --ip-permissions "$(aws ec2 describe-security-groups --region "$REGION" --group-ids "$SG_ID" \
    --query 'SecurityGroups[0].IpPermissions' --output json)" 2>/dev/null || true

aws ec2 authorize-security-group-ingress --region "$REGION" --group-id "$SG_ID" \
  --ip-permissions "[
    {\"IpProtocol\":\"tcp\",\"FromPort\":22,\"ToPort\":22,
     \"IpRanges\":[{\"CidrIp\":\"${MY_IP}/32\",\"Description\":\"SSH\"}]},
    {\"IpProtocol\":\"tcp\",\"FromPort\":${SS_PORT},\"ToPort\":${SS_PORT},
     \"IpRanges\":[{\"CidrIp\":\"${MY_IP}/32\",\"Description\":\"ss-2022\"}]},
    {\"IpProtocol\":\"udp\",\"FromPort\":${SS_PORT},\"ToPort\":${SS_PORT},
     \"IpRanges\":[{\"CidrIp\":\"${MY_IP}/32\",\"Description\":\"ss-2022 udp\"}]}
  ]" >/dev/null

# ─── user-data：装 sing-box 做 SS2022 inbound ─────────────────────
USER_DATA=$(cat <<EOF
#!/bin/bash
set -e
apt-get update -qq
apt-get install -y curl gnupg

curl -fsSL https://sing-box.app/gpg.key | gpg --dearmor -o /usr/share/keyrings/sagernet.gpg
echo "deb [arch=\$(dpkg --print-architecture) signed-by=/usr/share/keyrings/sagernet.gpg] https://deb.sagernet.org/ * *" > /etc/apt/sources.list.d/sagernet.list
apt-get update -qq
apt-get install -y sing-box

mkdir -p /etc/sing-box
cat > /etc/sing-box/config.json <<JSON
{
  "log": { "level": "info", "timestamp": true },
  "inbounds": [
    {
      "type": "shadowsocks",
      "tag": "ss-in",
      "listen": "::",
      "listen_port": ${SS_PORT},
      "method": "${SS_METHOD}",
      "password": "${SS_PASS}"
    }
  ],
  "outbounds": [
    { "type": "direct", "tag": "direct" }
  ]
}
JSON

systemctl enable --now sing-box
EOF
)

# ─── Launch ──────────────────────────────────────────────────────
log "启动实例..."
INSTANCE_ID="$(aws ec2 run-instances --region "$REGION" \
  --image-id "$AMI_ID" --instance-type "$INSTANCE_TYPE" \
  --key-name "$KEY_NAME" --security-group-ids "$SG_ID" \
  --user-data "$USER_DATA" \
  --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=$INSTANCE_NAME}]" \
  --query 'Instances[0].InstanceId' --output text)"
log "实例 ID: $INSTANCE_ID"

aws ec2 wait instance-running --region "$REGION" --instance-ids "$INSTANCE_ID"

log "分配 EIP"
ALLOC="$(aws ec2 allocate-address --region "$REGION" --domain vpc)"
EIP="$(echo "$ALLOC" | jq -r .PublicIp)"
ALLOC_ID="$(echo "$ALLOC" | jq -r .AllocationId)"
aws ec2 associate-address --region "$REGION" \
  --instance-id "$INSTANCE_ID" --allocation-id "$ALLOC_ID" >/dev/null

cat > ./.kiro-proxy.env <<EOF
# kiro-proxy 部署信息（勿提交）
KIRO_EC2_HOST=${EIP}
KIRO_SS_PORT=${SS_PORT}
KIRO_SS_METHOD=${SS_METHOD}
KIRO_SS_PASS=${SS_PASS}
INSTANCE_ID=${INSTANCE_ID}
ALLOC_ID=${ALLOC_ID}
SG_ID=${SG_ID}
REGION=${REGION}
SSH_KEY=${SSH_KEY}
EOF
chmod 600 ./.kiro-proxy.env

# 同步一份给 .app 用
mkdir -p "$HOME/.kiro-proxy"
cp ./.kiro-proxy.env "$HOME/.kiro-proxy/config.env"
chmod 600 "$HOME/.kiro-proxy/config.env"

cat <<MSG

╭──────────────────────────────────────────────────────────────
│ 部署完成 (user-data 正在后台装 sing-box，约 2 分钟)
│
│ EC2      : $EIP:$SS_PORT
│ 方法     : $SS_METHOD
│ 密码     : $SS_PASS
│ 配置     : ./.kiro-proxy.env + ~/.kiro-proxy/config.env
│
│ 验证 (约 2 分钟后):
│   ssh -i $SSH_KEY ubuntu@$EIP 'sudo systemctl status sing-box'
│
│ 启动 app:
│   ./scripts/build-app.sh
│   open dist/KiroProxy.app
╰──────────────────────────────────────────────────────────────
MSG
