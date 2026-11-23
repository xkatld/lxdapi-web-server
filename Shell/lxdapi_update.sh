#!/bin/bash

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log() { echo -e "$1"; }
ok() { log "${GREEN}[OK]${NC} $1"; }
info() { log "${BLUE}[INFO]${NC} $1"; }
warn() { log "${YELLOW}[WARN]${NC} $1"; }
err() { log "${RED}[ERR]${NC} $1"; exit 1; }

INSTALL_DIR="/opt/lxdapi"
CONFIG_FILE="$INSTALL_DIR/configs/config.yaml"
BACKUP_DIR="$INSTALL_DIR/backup"
SERVICE_NAME="lxdapi"

check_environment() {
    info "检测运行环境..."
    
    if [ ! -d "$INSTALL_DIR" ]; then
        err "未检测到 lxdapi 安装目录: $INSTALL_DIR"
    fi
    
    ok "环境检测通过"
}

detect_arch() {
    info "检测系统架构..."
    sys_arch=$(uname -m)
    case $sys_arch in
        x86_64)
            ARCH="amd64"
            ok "检测到架构: x86_64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ok "检测到架构: $sys_arch"
            ;;
        *)
            err "不支持的架构: $sys_arch"
            ;;
    esac
}

get_current_version() {
    if [ -f "$INSTALL_DIR/lxdapi-$ARCH" ]; then
        CURRENT_VERSION=$(stat -c %y "$INSTALL_DIR/lxdapi-$ARCH" 2>/dev/null | cut -d' ' -f1)
    else
        CURRENT_VERSION="未安装"
    fi
    info "当前版本: $CURRENT_VERSION"
}

get_latest_version() {
    info "获取最新版本..."
    
    while true; do
        read -rp "$(echo -e "${GREEN}请选择下载源 github/gitee [github]: ${NC}")" download_source
        download_source=${download_source:-github}
        if [[ "$download_source" =~ ^(github|gitee)$ ]]; then
            break
        else
            warn "请输入 github 或 gitee"
        fi
    done
    
    if [[ "$download_source" == "github" ]]; then
        LATEST_VERSION=$(curl -s https://api.github.com/repos/xkatld/lxdapi-web-server/releases/latest | grep '"tag_name"' | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')
        BASE_URL="https://github.com/xkatld/lxdapi-web-server/releases/download"
    else
        LATEST_VERSION=$(curl -s https://gitee.com/api/v5/repos/xkatld/lxdapi-web-server/releases/latest | grep '"tag_name"' | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')
        BASE_URL="https://gitee.com/xkatld/lxdapi-web-server/releases/download"
    fi
    
    if [ -z "$LATEST_VERSION" ]; then
        err "无法获取最新版本信息，请检查网络连接"
    fi
    
    info "最新版本: $LATEST_VERSION"
    read -rp "$(echo -e "${GREEN}请输入更新版本 [$LATEST_VERSION]: ${NC}")" UPDATE_VERSION
    UPDATE_VERSION=${UPDATE_VERSION:-$LATEST_VERSION}
    ok "更新版本: $UPDATE_VERSION"
}

stop_service() {
    info "停止 $SERVICE_NAME 服务..."
    
    if systemctl is-active --quiet $SERVICE_NAME; then
        systemctl stop $SERVICE_NAME
        sleep 2
        if systemctl is-active --quiet $SERVICE_NAME; then
            err "无法停止 $SERVICE_NAME 服务"
        fi
        ok "服务已停止"
    else
        warn "服务未运行"
    fi
}

backup_files() {
    info "备份当前文件..."
    
    BACKUP_TIME=$(date +%Y%m%d_%H%M%S)
    BACKUP_PATH="$BACKUP_DIR/$BACKUP_TIME"
    mkdir -p "$BACKUP_PATH"
    
    cp "$CONFIG_FILE" "$BACKUP_PATH/config.yaml"
    ok "配置文件已备份到: $BACKUP_PATH/config.yaml"
    
    if [ -f "$INSTALL_DIR/lxdapi-$ARCH" ]; then
        cp "$INSTALL_DIR/lxdapi-$ARCH" "$BACKUP_PATH/lxdapi-$ARCH"
        ok "二进制文件已备份"
    fi
    
    if [ -d "$INSTALL_DIR/certs" ]; then
        cp -r "$INSTALL_DIR/certs" "$BACKUP_PATH/certs"
        ok "证书文件已备份"
    fi
    
    if [ -f "$INSTALL_DIR/lxdapi.db" ]; then
        cp "$INSTALL_DIR/lxdapi.db" "$BACKUP_PATH/lxdapi.db"
        ok "数据库文件已备份"
    fi
}

download_latest() {
    info "下载版本 $UPDATE_VERSION..."
    
    DOWNLOAD_URL="${BASE_URL}/${UPDATE_VERSION}/lxdapi-linux-${ARCH}.tar.gz"
    info "下载地址: $DOWNLOAD_URL"
    
    TEMP_FILE=$(mktemp)
    TEMP_DIR=$(mktemp -d)
    
    if wget -q --show-progress -O "$TEMP_FILE" "$DOWNLOAD_URL" 2>&1; then
        ok "下载完成"
    else
        rm -f "$TEMP_FILE"
        rm -rf "$TEMP_DIR"
        err "下载失败"
    fi
    
    info "解压文件..."
    tar -xzf "$TEMP_FILE" -C "$TEMP_DIR" --strip-components=1
    
    if [ ! -f "$TEMP_DIR/lxdapi-$ARCH" ]; then
        rm -f "$TEMP_FILE"
        rm -rf "$TEMP_DIR"
        err "解压后未找到可执行文件"
    fi
    
    ok "解压完成"
    
    info "更新文件..."
    cp "$TEMP_DIR/lxdapi-$ARCH" "$INSTALL_DIR/lxdapi-$ARCH"
    chmod +x "$INSTALL_DIR/lxdapi-$ARCH"
    
    rm -f "$TEMP_FILE"
    rm -rf "$TEMP_DIR"
    
    ok "文件更新完成"
}

check_acme_config() {
    info "检查配置文件中的 plugins.acme 配置节..."

    if [ ! -f "$CONFIG_FILE" ]; then
        warn "未找到配置文件: $CONFIG_FILE"
        return
    fi

    if ! grep -Eq '^[[:space:]]*plugins:[[:space:]]*(\{\})?[[:space:]]*(#.*)?$' "$CONFIG_FILE"; then
        if [ -s "$CONFIG_FILE" ] && [ -n "$(tail -c 1 "$CONFIG_FILE")" ]; then
            printf '\n' >> "$CONFIG_FILE"
        fi
        cat >> "$CONFIG_FILE" << 'EOF'

plugins:
  acme:
    enabled: false
    cert_dir: "certs/acme"
EOF
        warn "检测到缺少 plugins 配置节，已自动补全 plugins.acme 默认配置"
    else
        local tmp_cfg
        tmp_cfg=$(mktemp)
        awk '
            BEGIN {
                in_plugins = 0
                in_acme = 0
                plugins_indent = ""
                sub_indent = ""
                acme_indent = ""
                acme_sub_indent = ""
                has_acme = 0
                has_enabled = 0
                has_cert_dir = 0
                acme_inserted = 0
            }
            function indent_of(s,    m) {
                match(s, /^[[:space:]]*/)
                return substr(s, 1, RLENGTH)
            }
            function flush_acme_missing() {
                if (in_acme) {
                    if (acme_sub_indent == "") {
                        acme_sub_indent = acme_indent "  "
                    }
                    if (!has_enabled) {
                        print acme_sub_indent "enabled: false"
                    }
                    if (!has_cert_dir) {
                        print acme_sub_indent "cert_dir: \"certs/acme\""
                    }
                    in_acme = 0
                }
            }
            function flush_plugins_missing() {
                flush_acme_missing()
                if (in_plugins && !has_acme && !acme_inserted) {
                    if (sub_indent == "") {
                        sub_indent = plugins_indent "  "
                    }
                    print sub_indent "acme:"
                    print sub_indent "  enabled: false"
                    print sub_indent "  cert_dir: \"certs/acme\""
                    acme_inserted = 1
                }
                in_plugins = 0
            }
            {
                raw = $0
                cur_indent = indent_of(raw)
                if (raw ~ /^[[:space:]]*plugins:[[:space:]]*(\{\})?[[:space:]]*(#.*)?$/) {
                    plugins_indent = cur_indent
                    in_plugins = 1
                    sub_indent = ""
                    has_acme = 0
                    acme_inserted = 0
                    if (raw ~ /\{\}/) {
                        sub(/\{\}/, "", raw)
                        sub(/[[:space:]]+$/, "", raw)
                    }
                    print raw
                    next
                }
                if (in_plugins) {
                    if (raw !~ /^[[:space:]]*$/ && raw !~ /^[[:space:]]*#/ && length(cur_indent) <= length(plugins_indent)) {
                        flush_plugins_missing()
                    } else {
                        if (raw !~ /^[[:space:]]*$/ && raw !~ /^[[:space:]]*#/) {
                            if (sub_indent == "") {
                                sub_indent = cur_indent
                            }
                            if (in_acme && length(cur_indent) <= length(acme_indent)) {
                                flush_acme_missing()
                            }
                            if (!in_acme && length(cur_indent) == length(sub_indent) && raw ~ /^[[:space:]]*acme:[[:space:]]*(\{\})?[[:space:]]*(#.*)?$/) {
                                has_acme = 1
                                in_acme = 1
                                acme_indent = cur_indent
                                acme_sub_indent = ""
                                has_enabled = 0
                                has_cert_dir = 0
                                if (raw ~ /\{\}/) {
                                    sub(/\{\}/, "", raw)
                                    sub(/[[:space:]]+$/, "", raw)
                                }
                                print raw
                                next
                            }
                            if (in_acme && length(cur_indent) > length(acme_indent)) {
                                if (acme_sub_indent == "") {
                                    acme_sub_indent = cur_indent
                                }
                                if (raw ~ /^[[:space:]]*enabled:[[:space:]]*/) {
                                    has_enabled = 1
                                }
                                if (raw ~ /^[[:space:]]*cert_dir:[[:space:]]*/) {
                                    has_cert_dir = 1
                                }
                            }
                        }
                        print raw
                        next
                    }
                }
                print raw
            }
            END {
                if (in_plugins) {
                    flush_plugins_missing()
                }
            }
        ' "$CONFIG_FILE" > "$tmp_cfg" && mv "$tmp_cfg" "$CONFIG_FILE"
        ok "已检查并幂等补全配置文件中的 plugins.acme 配置节"
    fi

    if awk '
        /^[[:space:]]*plugins:/ { in_plugins=1; p_ind=match($0, /[^[:space:]]/); next }
        in_plugins && !/^[[:space:]]*$/ && !/^[[:space:]]*#/ && match($0, /[^[:space:]]/) <= p_ind { in_plugins=0; in_acme=0 }
        in_plugins && /^[[:space:]]+acme:/ { in_acme=1; a_ind=match($0, /[^[:space:]]/); next }
        in_acme && !/^[[:space:]]*$/ && !/^[[:space:]]*#/ && match($0, /[^[:space:]]/) <= a_ind { in_acme=0 }
        in_acme && /^[[:space:]]+enabled:[[:space:]]*true/ { enabled=1 }
        END { exit(enabled ? 0 : 1) }
    ' "$CONFIG_FILE"; then
        if [ ! -d "/var/www/html" ]; then
            mkdir -p /var/www/html/.well-known/acme-challenge
            chmod -R 755 /var/www/html
            info "已自动创建 ACME 验证目录: /var/www/html"
        fi
    fi
}

start_service() {
    info "启动 $SERVICE_NAME 服务..."
    
    systemctl daemon-reload
    systemctl start $SERVICE_NAME
    
    info "等待服务启动..."
    for i in {1..10}; do
        printf "\r[%-10s] %d/10s" "$(printf '#%.0s' $(seq 1 $i))" "$i"
        sleep 1
    done
    echo
    
    if systemctl is-active --quiet $SERVICE_NAME; then
        ok "服务已启动"
    else
        warn "服务启动失败，查看日志:"
        journalctl -u $SERVICE_NAME -n 10 --no-pager
        err "请检查配置或手动启动服务"
    fi
}

show_result() {
    echo
    echo "========================================"
    echo "        lxdapi 更新完成"
    echo "========================================"
    echo
    info "更新前版本: $CURRENT_VERSION"
    info "更新后版本: $UPDATE_VERSION"
    info "备份目录: $BACKUP_PATH"
    echo
    info "===== 服务状态 ====="
    systemctl status $SERVICE_NAME --no-pager | head -10
}

rollback() {
    warn "执行回滚..."
    
    if [ -z "$BACKUP_PATH" ] || [ ! -d "$BACKUP_PATH" ]; then
        err "无法回滚：备份目录不存在"
    fi
    
    if [ -f "$BACKUP_PATH/lxdapi-$ARCH" ]; then
        cp "$BACKUP_PATH/lxdapi-$ARCH" "$INSTALL_DIR/lxdapi-$ARCH"
        chmod +x "$INSTALL_DIR/lxdapi-$ARCH"
        ok "已恢复二进制文件"
    fi
    
    systemctl start $SERVICE_NAME
    ok "回滚完成"
}

main() {
    echo
    echo "========================================"
    echo "        LXDAPI 更新脚本"
    echo "        by Github-xkatld"
    echo "========================================"
    echo
    
    check_environment
    detect_arch
    get_current_version
    get_latest_version
    
    echo
    read -rp "$(echo -e "${GREEN}确认更新? (y/n) [y]: ${NC}")" confirm
    confirm=${confirm:-y}
    
    if [[ ! "$confirm" =~ ^[yY]$ ]]; then
        info "已取消更新"
        exit 0
    fi
    
    echo
    stop_service
    backup_files
    
    if download_latest; then
        check_acme_config
        start_service
        show_result
    else
        rollback
    fi
}

main "$@"
