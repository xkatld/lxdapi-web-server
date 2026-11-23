#!/bin/bash

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_msg() {
    echo -e "$1"
}

log_ok() {
    log_msg "${GREEN}[OK]${NC} $1"
}

log_info() {
    log_msg "${BLUE}[INFO]${NC} $1" 
}

log_warn() {
    log_msg "${YELLOW}[WARN]${NC} $1"
}

log_err() {
    log_msg "${RED}[ERR]${NC} $1"
    exit 1
}

check_system() {
    log_info "正在检测系统版本与架构"
    
    local sys_pretty_name=`grep -i pretty_name /etc/os-release 2>/dev/null | cut -d "\"" -f2`
    sys_pretty_name=`echo "$sys_pretty_name" | tr '[:upper:]' '[:lower:]'`
    
    SYSTEM=""
    if [[ "$sys_pretty_name" =~ "debian" ]]; then
        SYSTEM="Debian"
    elif [[ "$sys_pretty_name" =~ "ubuntu" ]]; then
        SYSTEM="Ubuntu"
    fi
    
    if [ -z "$SYSTEM" ]; then
        log_err "此脚本仅支持 Debian 和 Ubuntu 系统"
    fi
    
    SYS_ARCH=`uname -m`
    if [[ "$SYS_ARCH" != "x86_64" && "$SYS_ARCH" != "aarch64" && "$SYS_ARCH" != "arm64" ]]; then
        log_err "不支持的架构: $SYS_ARCH"
    fi
    
    log_ok "系统检测通过: $SYSTEM $SYS_ARCH"
}

run_apt_noninteractive() {
    local apt_action=$1
    shift
    if [ "$apt_action" = "update" ]; then
        DEBIAN_FRONTEND=noninteractive apt-get update -o DPkg::Lock::Timeout=60 "$@"
        return $?
    fi
    DEBIAN_FRONTEND=noninteractive apt-get "$apt_action" -y -o DPkg::Lock::Timeout=60 -o Dpkg::Options::="--force-confdef" -o Dpkg::Options::="--force-confold" "$@"
}

check_and_update_kernel() {
    log_info "正在检测内核版本"

    local current_kernel=`uname -r`
    log_info "当前内核版本: $current_kernel"

    if [ "$SYSTEM" != "Debian" ]; then
        log_err "该内核更新策略仅支持 Debian 系统"
    fi

    local sys_arch=`dpkg --print-architecture`

    local is_cloud_kernel=0
    if [[ "$current_kernel" =~ "cloud" ]]; then
        is_cloud_kernel=1
        log_warn "检测到 cloud 精简内核，不支持 ZFS 编译"
    fi

    if [ $is_cloud_kernel -eq 0 ]; then
        log_ok "内核检测通过，当前已运行完整内核"
        return 0
    fi

    log_warn "需要更新为完整内核以支持 ZFS 编译"

    read -rp "是否确认安装并更新系统内核为最新完整内核？[y/n]: " confirm_install
    confirm_install=${confirm_install:-n}

    if [[ ! "$confirm_install" =~ ^[yY]$ ]]; then
        log_err "内核更新已取消，安装终止"
    fi

    log_info "开始更新系统内核"

    if ! run_apt_noninteractive update; then
        log_err "软件包索引更新失败"
    fi

    local img_pkg="linux-image-$sys_arch"
    local headers_pkg="linux-headers-$sys_arch"

    if run_apt_noninteractive install "$img_pkg" "$headers_pkg"; then
        log_info "正在卸载旧的 cloud 内核包"
        local cloud_kernel_pkgs=()
        mapfile -t cloud_kernel_pkgs < <(dpkg-query -W -f='${db:Status-Status}\t${Package}\n' 'linux-image-*cloud*' 'linux-headers-*cloud*' 2>/dev/null | awk -F'\t' '$1 == "installed" || $1 == "half-installed" || $1 == "half-configured" || $1 == "unpacked" {print $2}')

        if [ ${#cloud_kernel_pkgs[@]} -gt 0 ]; then
            if command -v debconf-set-selections >/dev/null 2>&1; then
                if ! printf '%s\n' "linux-base linux-base/removing-running-kernel boolean false" | debconf-set-selections; then
                    log_warn "debconf 预置失败，继续使用非交互模式执行卸载"
                fi
            fi
            if ! run_apt_noninteractive purge "${cloud_kernel_pkgs[@]}"; then
                log_err "旧 cloud 内核包卸载失败"
            fi
        fi

        log_info "正在清理旧内核文件"
        find /boot -maxdepth 1 -type f -name "vmlinuz-*cloud*" -delete
        find /boot -maxdepth 1 -type f -name "initrd.img-*cloud*" -delete
        find /boot -maxdepth 1 -type f -name "System.map-*cloud*" -delete
        find /boot -maxdepth 1 -type f -name "config-*cloud*" -delete

        log_info "正在清理无用软件包"
        if ! run_apt_noninteractive autoremove; then
            log_err "无用软件包清理失败"
        fi

        if ! update-grub; then
            log_err "GRUB 引导更新失败"
        fi
        log_ok "内核已更新为最新完整内核"
        log_warn "请手动执行 reboot 重启系统，重启后请重新运行该脚本"
        exit 0
    else
        log_err "内核包获取失败或安装未成功，操作已拦截"
    fi
}

install_zfs() {
    if [ "$SYSTEM" = "Ubuntu" ]; then
        log_ok "Ubuntu 系统自带 ZFS 模块，跳过安装"
        return 0
    fi

    log_info "开始安装 ZFS 通过 DKMS 编译"

    local current_kernel=`uname -r`

    if [[ "$current_kernel" =~ "cloud" ]]; then
        log_err "检测到 cloud 精简内核，不支持 ZFS 编译，请先更新为完整内核"
    fi

    log_info "正在更新软件包索引"
    if ! run_apt_noninteractive update; then
        log_err "软件包索引更新失败"
    fi

    log_info "正在安装当前内核头文件"
    if ! run_apt_noninteractive install "linux-headers-$current_kernel"; then
        log_err "内核头文件安装失败"
    fi

    log_info "正在安装 ZFS DKMS 模块和用户态工具"
    if ! run_apt_noninteractive install zfs-dkms zfsutils-linux; then
        log_err "ZFS 软件包安装失败"
    fi

    log_info "正在更新模块依赖"
    depmod -a $current_kernel

    log_info "正在加载 ZFS 内核模块"
    modprobe spl
    modprobe zfs

    if ! lsmod | grep -q zfs; then
        log_err "ZFS 模块加载失败"
    fi

    log_info "正在配置 ZFS 模块开机自动加载"
    echo "zfs" > /etc/modules-load.d/zfs.conf

    log_info "正在启用 ZFS 系统服务"
    systemctl enable zfs-import-cache zfs-import-scan zfs-mount zfs-share

    log_ok "ZFS 模块及工具安装部署完成"
}

view_zfs_status() {
    log_info "正在获取 ZFS 运行状态与版本信息"
    
    log_info "===== ZFS 工具与模块版本 ====="
    zfs --version
    
    log_info "===== ZFS 存储池状态 ====="
    zpool status
}

main() {
    log_msg "========================================"
    log_msg "        LXDAPI ZFS 自动安装脚本"
    log_msg "========================================"
    
    check_system
    check_and_update_kernel
    install_zfs
    view_zfs_status
    
    log_msg "========================================"
    log_msg "        ZFS 安装流程已全部完成"
    log_msg "========================================"
}

main
