package main

import (
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/gorilla/mux"

	common "github.com/tidepool-org/go-common"
	"github.com/tidepool-org/go-common/clients"
	"github.com/tidepool-org/go-common/clients/disc"
	"github.com/tidepool-org/go-common/clients/hakken"
	"github.com/tidepool-org/go-common/clients/highwater"
	"github.com/tidepool-org/go-common/clients/mongo"
	"github.com/tidepool-org/shoreline/oauth2"
	"github.com/tidepool-org/shoreline/user"
	"github.com/tidepool-org/shoreline/user/marketo"
)

type (
	Config struct {
		clients.Config
		Service disc.ServiceListing `json:"service"`
		Mongo   mongo.Config        `json:"mongo"`
		User    user.ApiConfig      `json:"user"`
		Oauth2  oauth2.ApiConfig    `json:"oauth2"`
	}
)

const (
	shoreline_service_prefix = "shoreline "
)

func main() {
	var config Config

	if err := common.LoadEnvironmentConfig([]string{"TIDEPOOL_SHORELINE_ENV", "TIDEPOOL_SHORELINE_SERVICE"}, &config); err != nil {
		log.Panic("Problem loading config", err)
	}

	// server secret may be passed via a separate env variable to accomodate easy secrets injection via Kubernetes
	serverSecret, found := os.LookupEnv("SERVER_SECRET")
	if found {
		config.User.ServerSecret = serverSecret
	}

	userSecret, found := os.LookupEnv("API_SECRET")
	if found {
		config.User.Secret = userSecret
	}

	longTermKey, found := os.LookupEnv("LONG_TERM_KEY")
	if found {
		config.User.LongTermKey = longTermKey
	}

	verificationSecret, found := os.LookupEnv("VERIFICATION_SECRET")
	if found {
		config.User.VerificationSecret = verificationSecret
	}
	clinicDemoUserID, found := os.LookupEnv("DEMO_CLINIC_USER_ID")
	if found {
		config.User.ClinicDemoUserID = clinicDemoUserID
	}
	marketoID, found := os.LookupEnv("MARKETO_ID")
	if found {
		config.User.Marketo.ID = marketoID
	}
	marketoURL, found := os.LookupEnv("MARKETO_URL")
	if found {
		config.User.Marketo.URL = marketoURL
	}
	marketoSecret, found := os.LookupEnv("MARKETO_SECRET")
	if found {
		config.User.Marketo.Secret = marketoSecret
	}
	marketoClinicRole, found := os.LookupEnv("MARKETO_CLINIC_ROLE")
	if found {
		config.User.Marketo.ClinicRole = marketoClinicRole
	}
	marketoPatientRole, found := os.LookupEnv("MARKETO_PATIENT_ROLE")
	if found {
		config.User.Marketo.PatientRole = marketoPatientRole
	}
	unParsedTimeout, found := (os.LookupEnv("MARKETO_TIMEOUT"))
	parsedTimeout64, err := strconv.ParseInt(unParsedTimeout, 10, 32)
	parsedTimeout := uint(parsedTimeout64)
	if err != nil {
		log.Println(err)
	}
	if found {
		config.User.Marketo.Timeout = parsedTimeout
	}

	salt, found := os.LookupEnv("SALT")
	if found {
		config.User.Salt = salt
	}

	config.Mongo.FromEnv()

	/*
	 * Hakken setup
	 */
	hakkenClient := hakken.NewHakkenBuilder().
		WithConfig(&config.HakkenConfig).
		Build()

	if !config.HakkenConfig.SkipHakken {
		if err := hakkenClient.Start(); err != nil {
			log.Fatal(shoreline_service_prefix, err)
		}
		defer hakkenClient.Close()
	} else {
		log.Print("skipping hakken service")
	}

	/*
	 * Clients
	 */

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	httpClient := &http.Client{Transport: tr}

	highwater := highwater.NewHighwaterClientBuilder().
		WithHostGetter(config.HighwaterConfig.ToHostGetter(hakkenClient)).
		WithHttpClient(httpClient).
		WithConfig(&config.HighwaterConfig.HighwaterClientConfig).
		Build()

	rtr := mux.NewRouter()

	/*
	 * User-Api setup
	 */

	log.Print(shoreline_service_prefix, "adding", user.USER_API_PREFIX)

	logger := log.New(os.Stdout, user.USER_API_PREFIX, log.LstdFlags|log.Lshortfile)
	miniConfig := marketo.Miniconfig(*config.User.Marketo)
	client, err := marketo.Client(miniConfig)
	marketoManager, err := marketo.NewManager(logger, config.User.Marketo, client)
	if err != nil {
		logger.Println("WARNING: Marketo Manager not configured;", err)
	}

	clientStore := user.NewMongoStoreClient(&config.Mongo)
	userapi := user.InitApi(config.User, logger, clientStore, highwater, marketoManager)
	userapi.SetHandlers("", rtr)

	userClient := user.NewUserClient(userapi)

	permsClient := clients.NewGatekeeperClientBuilder().
		WithHostGetter(config.GatekeeperConfig.ToHostGetter(hakkenClient)).
		WithHttpClient(httpClient).
		WithTokenProvider(userClient).
		Build()

	log.Print(shoreline_service_prefix, "adding", "permsClient")
	userapi.AttachPerms(permsClient)

	/*
	 * Oauth setup
	 */

	log.Print(shoreline_service_prefix, "adding", oauth2.OAUTH2_API_PREFIX)

	oauthapi := oauth2.InitApi(config.Oauth2, oauth2.NewOAuthStorage(&config.Mongo), userClient, permsClient)
	oauthapi.SetHandlers("", rtr)

	oauthClient := oauth2.NewOAuth2Client(oauthapi)

	log.Print(shoreline_service_prefix, oauth2.OAUTH2_API_PREFIX, "adding oauthClient")
	userapi.AttachOauth(oauthClient)

	/*
	 * Serve it up and publish
	 */
	done := make(chan bool)
	server := common.NewServer(&http.Server{
		Addr:    config.Service.GetPort(),
		Handler: rtr,
	})

	var start func() error
	if config.Service.Scheme == "https" {
		sslSpec := config.Service.GetSSLSpec()
		start = func() error { return server.ListenAndServeTLS(sslSpec.CertFile, sslSpec.KeyFile) }
	} else {
		start = func() error { return server.ListenAndServe() }
	}
	if err := start(); err != nil {
		log.Fatal(shoreline_service_prefix, err)
	}

	hakkenClient.Publish(&config.Service)

	signals := make(chan os.Signal, 40)
	signal.Notify(signals)
	go func() {
		for {
			sig := <-signals
			log.Printf(shoreline_service_prefix+"Got signal [%s]", sig)

			if sig == syscall.SIGINT || sig == syscall.SIGTERM {
				server.Close()
				done <- true
			}
		}
	}()

	<-done

}
