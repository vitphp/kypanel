package service

import "testing"

func TestCheckForbidden(t *testing.T) {
	blocked := []string{
		"rm -rf /",
		"rm -r -f /",
		"rm -rf -- /",
		"sudo rm -rf /etc",
		"cd /www && rm -rf *",
		"rm -rf ../backup",
		"reboot", "shutdown -h now", "halt", "poweroff", "init 0",
		"mkfs.ext4 /dev/sdb", "dd if=/dev/zero of=/dev/sda",
		"chmod -R 777 /", "kill -9 1", ":(){ :|:& };:",
	}
	allowed := []string{
		"rm -f /tmp/x", "rm -r /tmp/x",
		"ls -la", "systemctl restart nginx",
		"tail -f /var/log/nginx/error.log", "grep -r foo /www/wwwroot",
		"docker ps", "df -h", "top -b -n 1",
	}
	for _, c := range blocked {
		if err := checkForbidden(c); err == nil {
			t.Errorf("应拦截但放行: %q", c)
		}
	}
	for _, c := range allowed {
		if err := checkForbidden(c); err != nil {
			t.Errorf("不应拦截: %q -> %v", c, err)
		}
	}
}

func TestMaskSecret(t *testing.T) {
	if MaskSecret("") != "" || MaskSecret("ab") != "****" || MaskSecret("abcdef") != "****cdef" {
		t.Fatal("MaskSecret 行为异常")
	}
}
