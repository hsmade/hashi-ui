package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/jippi/hashi-ui/backend/config"
	log "github.com/sirupsen/logrus"
)

func startLogging(logLevel string) {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		fmt.Printf("%s (%s)", err, logLevel)
		os.Exit(1)
	}

	log.SetLevel(level)
}

func main() {
	cfg := config.DefaultConfig()
	cfg.Parse()

	startLogging(cfg.LogLevel)

	log.Infof("-----------------------------------------------------------------------------")
	log.Infof("|                             HASHI UI                                      |")
	log.Infof("-----------------------------------------------------------------------------")
	if !cfg.HttpsEnable {
		log.Infof("| listen-address       : http://%-43s |", cfg.ListenAddress)
	} else {
		log.Infof("| listen-address      : https://%-43s  |", cfg.ListenAddress)
	}
	log.Infof("| server-certificate   : %-50s |", cfg.ServerCert)
	log.Infof("| server-key       	  : %-50s |", cfg.ServerKey)
	log.Infof("| proxy-address   	  : %-50s |", cfg.ProxyAddress)
	log.Infof("| log-level       	  : %-50s |", cfg.LogLevel)

	// Nomad
	log.Infof("| nomad-enable     	  : %-50t |", cfg.NomadEnable)
	if cfg.NomadReadOnly {
		log.Infof("| nomad-read-only      : %-50s |", "Yes")
	} else {
		log.Infof("| nomad-read-only      : %-50s |", "No (Hashi-UI can change Nomad state)")
	}
	log.Infof("| nomad-address        : %-50s |", cfg.NomadAddress)
	log.Infof("| nomad-ca-cert        : %-50s |", cfg.NomadCACert)
	log.Infof("| nomad-client-cert    : %-50s |", cfg.NomadClientCert)
	log.Infof("| nomad-client-key     : %-50s |", cfg.NomadClientKey)
	log.Infof("| nomad-skip-verify    : %-50t |", cfg.NomadSkipVerify)
	log.Infof("| nomad-hide-env-data  : %-50v |", cfg.NomadHideEnvData)
	if cfg.NomadSkipVerify {
		log.Infof("| nomad-skip-verify    : %-50s |", "Yes")
	} else {
		log.Infof("| nomad-skip-verify    : %-50s |", "No")
	}

	// Consul
	log.Infof("| consul-enable     	  : %-50t |", cfg.ConsulEnable)
	if cfg.ConsulReadOnly {
		log.Infof("| consul-read-only     : %-50s |", "Yes")
	} else {
		log.Infof("| consul-read-only     : %-50s |", "No (Hashi-UI can change Consul state)")
	}
	log.Infof("| consul-address       : %-50s |", cfg.ConsulAddress)
	log.Infof("| consul.acl-token     : %-50s |", cfg.ConsulACLToken)

	log.Infof("-----------------------------------------------------------------------------")
	log.Infof("")

	if !cfg.NomadEnable && !cfg.ConsulEnable {
		log.Fatal("Please enable at least Consul (--consul-enable) or Nomad (--nomad-enable)")
	}

	myAssetFS := assetFS()
	router := mux.NewRouter()

	if cfg.NomadEnable {
		router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			log.Infof("Redirecting / to /nomad")
			w.Write([]byte("<script>document.location.href='" + cfg.ProxyAddress + "/nomad'</script>"))
			return
		})

		router.HandleFunc("/ws/nomad", NomadHandler(cfg))
		router.HandleFunc("/ws/nomad/{region}", NomadHandler(cfg))
		router.HandleFunc("/nomad/{region}/download/{path:.*}", NomadDownloadFile(cfg))
	}

	if cfg.ConsulEnable {
		if !cfg.NomadEnable {
			router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				log.Infof("Redirecting / to /consul")
				http.Redirect(w, r, cfg.ProxyAddress+"/consul", 302)
			})
		}

		router.HandleFunc("/ws/consul", ConsulHandler(cfg))
		router.HandleFunc("/ws/consul/{region}", ConsulHandler(cfg))
	}

	router.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responseFile := "/index.html"

		if idx := strings.Index(r.URL.Path, "static/"); idx != -1 {
			responseFile = r.URL.Path[idx:]
		}

		if idx := strings.Index(r.URL.Path, "favicon.png"); idx != -1 {
			responseFile = "/favicon.png"
		}

		if idx := strings.Index(r.URL.Path, "config.js"); idx != -1 {
			response := make([]string, 0)
			response = append(response, fmt.Sprintf("window.CONSUL_ENABLED=%s", strconv.FormatBool(cfg.ConsulEnable)))
			response = append(response, fmt.Sprintf("window.CONSUL_READ_ONLY=%s", strconv.FormatBool(cfg.ConsulReadOnly)))

			response = append(response, fmt.Sprintf("window.NOMAD_ENABLED=%s", strconv.FormatBool(cfg.NomadEnable)))
			response = append(response, fmt.Sprintf("window.NOMAD_READ_ONLY=%s", strconv.FormatBool(cfg.NomadReadOnly)))

			enabledServices := make([]string, 0)
			if cfg.ConsulEnable {
				enabledServices = append(enabledServices, "'consul'")
			}
			if cfg.NomadEnable {
				enabledServices = append(enabledServices, "'nomad'")
			}

			response = append(response, fmt.Sprintf("window.ENABLED_SERVICES=[%s]", strings.Join(enabledServices, ",")))

			var endpointURL string
			if cfg.ProxyAddress != "" {
				endpointURL = fmt.Sprintf("\"%s\"", strings.TrimSuffix(cfg.ProxyAddress, "/"))
			} else {
				endpointURL = "document.location.protocol + '//' + document.location.hostname + ':' + (window.NOMAD_ENDPOINT_PORT || document.location.port)"
			}

			response = append(response, fmt.Sprintf("window.NOMAD_ENDPOINT=%s", endpointURL))

			w.Header().Set("Content-Type", "application/javascript")
			w.Write([]byte(strings.Join(response, "\n")))
			return
		}

		if bs, err := myAssetFS.Open(responseFile); err != nil {
			log.Errorf("%s: %s", responseFile, err)
		} else {
			stat, err := bs.Stat()
			if err != nil {
				log.Errorf("Failed to stat %s: %s", responseFile, err)
			} else {
				http.ServeContent(w, r, responseFile[1:], stat.ModTime(), bs)
			}
		}
	})

	log.Infof("Listening ...")
	var err error
	if cfg.HttpsEnable {
		if cfg.ServerCert == "" || cfg.ServerKey == "" {
			log.Fatal("Using https protocol but server certificate or key were not specified.")
		}
		err = http.ListenAndServeTLS(cfg.ListenAddress, cfg.ServerCert, cfg.ServerKey, router)
	} else {
		err = http.ListenAndServe(cfg.ListenAddress, router)
	}
	if err != nil {
		log.Fatal(err)
	}
}
