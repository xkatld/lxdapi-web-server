#!/bin/bash

cd /root >/dev/null 2>&1

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

REGEX=("debian|astra" "ubuntu")
RELEASE=("Debian" "Ubuntu")
CMD=("$(grep -i pretty_name /etc/os-release 2>/dev/null | cut -d \" -f2)" "$(lsb_release -sd 2>/dev/null)")
SYS="${CMD[0]}"
[[ -n $SYS ]] || exit 1

for ((int = 0; int < ${#REGEX[@]}; int++)); do
    if [[ $(echo "$SYS" | tr '[:upper:]' '[:lower:]') =~ ${REGEX[int]} ]]; then
        SYSTEM="${RELEASE[int]}"
        [[ -n $SYSTEM ]] && break
    fi
done

if [[ "$SYSTEM" != "Debian" && "$SYSTEM" != "Ubuntu" ]]; then
    echo -e "${RED}[ERR]${NC} 此脚本仅支持 Debian 和 Ubuntu 系统"
    exit 1
fi

log() { echo -e "$1"; }
ok() { log "${GREEN}[OK]${NC} $1"; }
info() { log "${BLUE}[INFO]${NC} $1"; }
warn() { log "${YELLOW}[WARN]${NC} $1"; }
err() { log "${RED}[ERR]${NC} $1"; exit 1; }

reading() { read -rp "$(echo -e "${GREEN}$1${NC}")" "$2"; }

install_package() {
    package_name=$1
    if dpkg -l 2>/dev/null | grep -q "^ii.*$package_name"; then
        ok "$package_name 已安装"
    else
        apt-get install -y $package_name >/dev/null 2>&1
        if [ $? -ne 0 ]; then
            apt-get install -y $package_name --fix-missing >/dev/null 2>&1
        fi
        if dpkg -l 2>/dev/null | grep -q "^ii.*$package_name"; then
            ok "$package_name 已安装"
        else
            warn "$package_name 安装失败"
        fi
    fi
}

install_base_packages() {
	info "更新软件包列表..."
	apt-get update >/dev/null 2>&1
	apt-get autoremove -y >/dev/null 2>&1

	info "安装基础软件包..."
	DEBIAN_FRONTEND=noninteractive apt-get install -y unzip e2fsprogs bc fdisk parted >/dev/null 2>&1
	ok "软件包安装完成"
}

deploy_lxdapi() {
    info "检测系统架构..."
    sys_arch=$(uname -m)
    case $sys_arch in
        x86_64)
            arch="amd64"
            ok "检测到架构: x86_64"
            ;;
        aarch64|arm64)
            arch="arm64"
            ok "检测到架构: $sys_arch"
            ;;
        *)
            err "不支持的架构: $sys_arch"
            ;;
    esac
    
    while true; do
        reading "请选择下载源 github/gitee [github]：" download_source
        download_source=${download_source:-github}
        if [[ "$download_source" =~ ^(github|gitee)$ ]]; then
            break
        else
            warn "请输入 github 或 gitee"
        fi
    done
    
    if [[ "$download_source" == "github" ]]; then
        latest_tag=$(curl -s https://api.github.com/repos/xkatld/lxdapi-web-server/releases/latest | grep '"tag_name"' | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')
        base_url="https://github.com/xkatld/lxdapi-web-server/releases/download"
    else
        latest_tag=$(curl -s https://gitee.com/api/v5/repos/xkatld/lxdapi-web-server/releases/latest | grep '"tag_name"' | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')
        base_url="https://gitee.com/xkatld/lxdapi-web-server/releases/download"
    fi
    
    if [ -z "$latest_tag" ]; then
        err "无法获取最新版本信息"
    fi
    
    info "最新版本: $latest_tag"
    reading "请输入安装版本 [$latest_tag]：" install_version
    install_version=${install_version:-$latest_tag}
    ok "安装版本: $install_version"
    
    download_url="${base_url}/${install_version}/lxdapi-linux-${arch}.tar.gz"
    
    info "下载 lxdapi..."
    info "下载地址: $download_url"
    
    temp_file=$(mktemp)
    if wget -q --show-progress -O "$temp_file" "$download_url" 2>&1; then
        ok "下载完成"
    else
        rm -f "$temp_file"
        err "下载失败"
    fi
    
    info "解压到 /opt/lxdapi..."
    mkdir -p /opt/lxdapi
    tar -xzf "$temp_file" -C /opt/lxdapi --strip-components=1
    rm -f "$temp_file"
}

configure_lxdapi() {
    info "配置 lxdapi..."
    
    config_file="/opt/lxdapi/configs/config.yaml"
    
    if [ ! -f "$config_file" ]; then
        err "配置文件不存在: $config_file"
    fi
    
    reading "请输入服务端口 [8443]：" server_port
    server_port=${server_port:-8443}
    
    reading "请输入API密钥 [随机生成]：" api_hash
    if [ -z "$api_hash" ]; then
        api_hash=$(openssl rand -hex 16)
        ok "API密钥已生成: $api_hash"
    fi
    
    reading "请输入管理员用户名 [admin]：" admin_user
    admin_user=${admin_user:-admin}
    
    reading "请输入管理员密码 [随机生成]：" admin_pass
    if [ -z "$admin_pass" ]; then
        admin_pass=$(openssl rand -hex 8)
        ok "管理员密码已生成: $admin_pass"
    fi
    
    session_secret=$(openssl rand -hex 16)
    
    reading "请输入流量采集间隔秒数 [20]：" traffic_interval
    traffic_interval=${traffic_interval:-20}
    
    reading "请输入流量批量更新数量 [10]：" traffic_batch_size
    traffic_batch_size=${traffic_batch_size:-10}
    
    reading "请输入任务自动清理天数 [7]：" auto_cleanup_days
    auto_cleanup_days=${auto_cleanup_days:-7}
   
    reading "是否启用 Nginx 反向代理插件？ y/n [y]：" nginx_enabled
    nginx_enabled=${nginx_enabled:-y}
    if [[ "$nginx_enabled" =~ ^[yY]$ ]]; then
    	install_package nginx
    	systemctl enable nginx >/dev/null 2>&1
    	systemctl start nginx >/dev/null 2>&1
    	ok "nginx 已安装并启动"
    	nginx_enabled_value="true"
    else
    	warn "Nginx 已禁用"
    	nginx_enabled_value="false"
    fi

    reading "是否启用 ACME 自动证书插件？ y/n [y]：" acme_enabled
    acme_enabled=${acme_enabled:-y}
    if [[ "$acme_enabled" =~ ^[yY]$ ]]; then
        mkdir -p /var/www/html/.well-known/acme-challenge
        chmod -R 755 /var/www/html
        ok "ACME 验证目录已创建: /var/www/html"
        acme_enabled_value="true"
    else
        warn "ACME 自动证书插件已禁用"
        acme_enabled_value="false"
    fi

    if [[ "$nginx_enabled_value" == "true" ]]; then
        mkdir -p /var/www/html/.well-known/acme-challenge
        chmod -R 755 /var/www/html
        if ! grep -rq "\.well-known/acme-challenge" /etc/nginx/nginx.conf /etc/nginx/conf.d/ /etc/nginx/sites-enabled/ /etc/nginx/sites-available/ 2>/dev/null; then
            injected=0
            target_nginx_conf=""
            for candidate in /etc/nginx/sites-enabled/default /etc/nginx/sites-available/default /etc/nginx/conf.d/default.conf /etc/nginx/sites-enabled/* /etc/nginx/conf.d/*.conf; do
                if [ -f "$candidate" ] && grep -Eq 'listen[[:space:]]+([^;]*:)?80([[:space:];]|$)' "$candidate"; then
                    target_nginx_conf="$candidate"
                    break
                fi
            done
            if [ -n "$target_nginx_conf" ]; then
                tmp_nginx_conf=$(mktemp)
                cp "$target_nginx_conf" "${target_nginx_conf}.bak"
                awk '
                    BEGIN { in_server=0; has_80=0; done=0; depth=0 }
                    {
                        line = $0
                        if (!done && !in_server && line ~ /server[[:space:]]*\{/) {
                            in_server = 1
                            has_80 = 0
                            depth = 0
                        }
                        if (in_server) {
                            n_open = gsub(/\{/, "{", line)
                            n_close = gsub(/\}/, "}", line)
                            depth += n_open - n_close
                            if (line ~ /listen[[:space:]]+([^;]*:)?80([[:space:];]|$)/) {
                                has_80 = 1
                            }
                            if (has_80 && !done && depth <= 0) {
                                print "    location ^~ /.well-known/acme-challenge/ {"
                                print "        root /var/www/html;"
                                print "        default_type \"text/plain\";"
                                print "        allow all;"
                                print "    }"
                                done = 1
                                in_server = 0
                            }
                        }
                        print $0
                    }
                ' "$target_nginx_conf" > "$tmp_nginx_conf"
                if [ -s "$tmp_nginx_conf" ]; then
                    mv "$tmp_nginx_conf" "$target_nginx_conf"
                    if nginx -t >/dev/null 2>&1; then
                        rm -f "${target_nginx_conf}.bak"
                        injected=1
                    else
                        mv "${target_nginx_conf}.bak" "$target_nginx_conf"
                    fi
                else
                    rm -f "$tmp_nginx_conf" "${target_nginx_conf}.bak"
                fi
            fi
            if [ "$injected" -eq 0 ]; then
                if grep -rq "default_server" /etc/nginx/ 2>/dev/null; then
                    listen_v4="listen 80;"
                    listen_v6="listen [::]:80;"
                else
                    listen_v4="listen 80 default_server;"
                    listen_v6="listen [::]:80 default_server;"
                fi
                cat > /etc/nginx/conf.d/acme-challenge.conf << EOF
server {
    $listen_v4
    $listen_v6
    server_name _;

    location ^~ /.well-known/acme-challenge/ {
        root /var/www/html;
        default_type "text/plain";
        allow all;
    }

    location / {
        return 404;
    }
}
EOF
            fi
        fi
        if nginx -t >/dev/null 2>&1; then
            systemctl reload nginx >/dev/null 2>&1
            ok "Nginx 80 端口 ACME 验证规则已就绪"
        else
            warn "Nginx 配置校验未通过，请检查 /.well-known/acme-challenge/ 配置"
        fi
    fi

    task_backend="memory"
    db_type="sqlite"

    info "写入配置文件..."
    sed -i "s|__SERVER_PORT__|$server_port|g" "$config_file"
    sed -i "s|__API_HASH__|$api_hash|g" "$config_file"
    sed -i "s|__ADMIN_USER__|$admin_user|g" "$config_file"
    sed -i "s|__ADMIN_PASS__|$admin_pass|g" "$config_file"
    sed -i "s|__SESSION_SECRET__|$session_secret|g" "$config_file"
    sed -i "s|__TRAFFIC_INTERVAL__|$traffic_interval|g" "$config_file"
    sed -i "s|__TRAFFIC_BATCH_SIZE__|$traffic_batch_size|g" "$config_file"
    sed -i "s|__AUTO_CLEANUP_DAYS__|$auto_cleanup_days|g" "$config_file"
    sed -i "s|__TASK_BACKEND__|$task_backend|g" "$config_file"
    sed -i "s|__DB_TYPE__|$db_type|g" "$config_file"
    sed -i "s|__REDIS_HOST__|localhost|g" "$config_file"
    sed -i "s|__REDIS_PORT__|6379|g" "$config_file"
    sed -i "s|__REDIS_PASSWORD__||g" "$config_file"
    sed -i "s|__REDIS_DB__|0|g" "$config_file"
    sed -i "s|__MYSQL_HOST__|localhost|g" "$config_file"
    sed -i "s|__MYSQL_PORT__|3306|g" "$config_file"
    sed -i "s|__MYSQL_USER__|root|g" "$config_file"
    sed -i "s|__MYSQL_PASSWORD__||g" "$config_file"
    sed -i "s|__MYSQL_DATABASE__|lxdapi|g" "$config_file"
    sed -i "s|__POSTGRES_HOST__|localhost|g" "$config_file"
    sed -i "s|__POSTGRES_PORT__|5432|g" "$config_file"
    sed -i "s|__POSTGRES_USER__|postgres|g" "$config_file"
    sed -i "s|__POSTGRES_PASSWORD__||g" "$config_file"
    sed -i "s|__POSTGRES_DATABASE__|lxdapi|g" "$config_file"
    sed -i "s|__POSTGRES_SSLMODE__|disable|g" "$config_file"
    sed -i "s|__NGINX_ENABLED__|$nginx_enabled_value|g" "$config_file"
    sed -i "s|__ACME_ENABLED__|$acme_enabled_value|g" "$config_file"
   
    ok "配置文件已更新 (已固定 SQLite & Memory 模式)"
   }

setup_lxdapi_service() {
    info "配置 lxdapi 系统服务..."
    
    config_file="/opt/lxdapi/configs/config.yaml"
    if [ ! -f "$config_file" ]; then
        err "配置文件不存在: $config_file"
    fi
    
    if grep -q "__SERVER_PORT__" "$config_file"; then
        err "配置文件未完成配置"
    fi
    
    sys_arch=$(uname -m)
    case $sys_arch in
        x86_64)
            exec_bin="/opt/lxdapi/lxdapi-amd64"
            ;;
        aarch64|arm64)
            exec_bin="/opt/lxdapi/lxdapi-arm64"
            ;;
        *)
            err "不支持的架构: $sys_arch"
            ;;
    esac
    
    service_file="/etc/systemd/system/lxdapi.service"
    
    cat > "$service_file" << EOF
[Unit]
Description=LXD API Server
After=network.target lxd.service
Wants=lxd.service

[Service]
Type=simple
User=root
WorkingDirectory=/opt/lxdapi
Environment="PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin"
ExecStart=$exec_bin
ExecReload=/bin/kill -HUP \$MAINPID
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF
    
    ok "服务文件已创建: $service_file"
    
    systemctl daemon-reload
    systemctl enable lxdapi
    systemctl start lxdapi
    
    info "等待服务启动..."
    for i in {1..10}; do
        printf "\r[%-10s] %d/10s" "$(printf '#%.0s' $(seq 1 $i))" "$i"
        sleep 1
    done
    echo
    
    if systemctl is-active --quiet lxdapi; then
        ok "lxdapi 服务已启动"
    else
        warn "lxdapi 服务启动失败"
        journalctl -u lxdapi -n 20 --no-pager
    fi
}

main() {
    echo
    echo "========================================"
    echo "        LXDAPI 安装脚本"
    echo "        by Github-xkatld"
    echo "========================================"
    echo
    
    echo "======== 步骤 1/5: 基础软件包安装 ========"
    reading "是否安装基础软件包？(y/n) [y]：" step1_confirm
    step1_confirm=${step1_confirm:-y}
    if [[ "$step1_confirm" =~ ^[yY]$ ]]; then
        install_base_packages
        ok "基础软件包安装完成"
    else
        info "已跳过基础软件包安装"
    fi
    echo

    echo "======== 步骤 2/5: 下载 ========"
    reading "是否下载 lxdapi？(y/n) [y]：" step2_confirm
    step2_confirm=${step2_confirm:-y}
    if [[ "$step2_confirm" =~ ^[yY]$ ]]; then
        deploy_lxdapi
        ok "下载完成"
    else
        info "已跳过下载"
    fi
    echo

    echo "======== 步骤 3/5: 配置 ========"
    reading "是否配置 lxdapi？(y/n) [y]：" step3_confirm
    step3_confirm=${step3_confirm:-y}
    if [[ "$step3_confirm" =~ ^[yY]$ ]]; then
        configure_lxdapi
        ok "配置完成"
    else
        info "已跳过配置"
    fi
    echo

    echo "======== 步骤 4/5: 启动服务 ========"
    reading "是否启动 lxdapi 服务？(y/n) [y]：" step4_confirm
    step4_confirm=${step4_confirm:-y}
    if [[ "$step4_confirm" =~ ^[yY]$ ]]; then
        setup_lxdapi_service
        ok "服务已启动"
    else
        info "已跳过服务启动"
    fi
    echo

    echo "======== 步骤 5/5: 完成 ========"
    echo
    echo "========================================"
    echo "        LXDAPI 安装完成"
    echo "========================================"
    echo
    info "服务端口: $server_port"
    info "API密钥: $api_hash"
    info "管理员用户: $admin_user"
    info "管理员密码: $admin_pass"
    info "任务队列: Memory"
    info "数据库类型: SQLite"
    info "流量采集间隔: ${traffic_interval}s"
    echo
    systemctl status lxdapi --no-pager | head -5
}

main
