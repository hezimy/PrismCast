package dlna

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	"github.com/hezimy/PrismCast/internal/applog"
)

// AddFirewallRules adds Windows Firewall rules for SSDP and UPnP.
// Uses FIXED rule names so they only need to be added once (survives restarts).
func AddFirewallRules(port int) bool {
	if runtime.GOOS != "windows" {
		return true
	}

	success := true

	// Rule 1: SSDP (UDP 1900) - critical for device discovery
	if !firewallRuleExists("PrismCast SSDP") {
		err := tryAddRule("PrismCast SSDP", "UDP", "1900")
		if err != nil {
			applog.Mainf("[WARN] SSDP防火墙规则添加失败!")
			applog.Mainf("[WARN] 错误: %s", err)
			applog.Mainf("[INFO] ========================================")
			applog.Mainf("[INFO] 手机搜不到设备的原因: UDP 1900 被防火墙拦截")
			applog.Mainf("[INFO] 解决方法 (任选其一):")
			applog.Mainf("[INFO]   A. 右键 PrismCast.exe → 以管理员身份运行")
			applog.Mainf("[INFO]   B. 手动执行以下命令(管理员PowerShell):")
			applog.Mainf("        netsh advfirewall firewall add rule name=\"PrismCast SSDP\" dir=in action=allow protocol=UDP localport=1900 profile=private")
			applog.Mainf("        netsh advfirewall firewall add rule name=\"PrismCast UPnP\" dir=in action=allow protocol=TCP localport=%d profile=private", port)
			applog.Mainf("[INFO] ========================================")
			success = false
		} else {
			applog.Mainf("[OK] 防火墙规则已添加: PrismCast SSDP (UDP 1900)")
		}
	} else {
		applog.Mainf("[OK] SSDP防火墙规则已存在 (UDP 1900)")
	}

	// Rule 2: UPnP HTTP (TCP port) - for device description/control
	// 规则名包含端口号，避免换端口后旧规则残留导致新端口未放行
	ruleName := fmt.Sprintf("PrismCast UPnP TCP %d", port)
	if !firewallRuleExists(ruleName) {
		portStr := fmt.Sprintf("%d", port)
		err := tryAddRule(ruleName, "TCP", portStr)
		if err != nil {
			applog.Mainf("[WARN] UPnP防火墙规则添加失败: %s", err)
			success = false
		} else {
			applog.Mainf("[OK] 防火墙规则已添加: %s (TCP %s)", ruleName, portStr)
		}
	} else {
		applog.Mainf("[OK] UPnP防火墙规则已存在 (%s)", ruleName)
	}

	return success
}

// tryAddRule attempts to add a firewall rule, trying netsh first then PowerShell
func tryAddRule(name, protocol, port string) error {
	// Method 1: netsh (traditional)
	err := runNetshRule(name, protocol, port)
	if err == nil {
		return nil
	}
	netshErr := err

	// Method 2: PowerShell New-NetFirewallRule (may work when netsh doesn't)
	err = runPSRule(name, protocol, port)
	if err == nil {
		return nil
	}

	// Both failed, return netsh error as primary
	return fmt.Errorf("netsh: %v; powershell: %v", netshErr, err)
}

func runNetshRule(name, protocol, port string) error {
	cmd := exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
		fmt.Sprintf("name=%s", name),
		"dir=in", "action=allow",
		fmt.Sprintf("protocol=%s", protocol),
		fmt.Sprintf("localport=%s", port),
		"profile=private",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

func runPSRule(name, protocol, port string) error {
	psCmd := fmt.Sprintf(
		`New-NetFirewallRule -DisplayName "%s" -Direction Inbound -Action Allow -Protocol %s -LocalPort %s -Profile Private`,
		name, protocol, port,
	)
	cmd := exec.Command("powershell", "-NoProfile", "-Command", psCmd)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

func firewallRuleExists(name string) bool {
	cmd := exec.Command("netsh", "advfirewall", "firewall", "show", "rule",
		fmt.Sprintf("name=%s", name))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(out) > 0 && strings.Contains(string(out), name)
}
