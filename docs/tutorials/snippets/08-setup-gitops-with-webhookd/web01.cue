package syslet

import syslettools "github.com/xchangeee/syslet/schema/tools@v0"

sysdef: containers: site: spec: {
	unit: Container: {
		Image: "docker.io/library/nginx:1.28"
		PublishPort: ["8080:80"]
		Secret: {
			"site-htpasswd": "uid=101,gid=101,mode=0400"
		}
	}
	configFiles: [{
		mountPath: "/etc/nginx/conf.d/default.conf"
		mode:      "0644"
		content: """
			server {
			    listen 80;
			    root /usr/share/nginx/html;
			    location /healthz { return 200 "ok"; }
			    location /admin/ {
			        auth_basic "admin";
			        auth_basic_user_file /run/secrets/site-htpasswd;
			    }
			}
			"""
	}]
}

sysdef: (syslettools.#SysdefLock & {in: containers: ["site"]}).out
