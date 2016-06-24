package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/elasticache"
	"github.com/frodenas/brokerapi"
	"github.com/pivotal-golang/lager"

	"github.com/apefactory/elasticache-broker/awselasticache"
	"github.com/apefactory/elasticache-broker/broker"
)

var (
	configFilePath string
	port           string

	logLevels = map[string]lager.LogLevel{
		"DEBUG": lager.DEBUG,
		"INFO":  lager.INFO,
		"ERROR": lager.ERROR,
		"FATAL": lager.FATAL,
	}
)

func init() {
	flag.StringVar(&configFilePath, "config", "", "Location of the config file")
	flag.StringVar(&port, "port", "3000", "Listen port")
}

func buildLogger(logLevel string) lager.Logger {
	laggerLogLevel, ok := logLevels[strings.ToUpper(logLevel)]
	if !ok {
		log.Fatal("Invalid log level: ", logLevel)
	}

	logger := lager.NewLogger("elasticache-broker")
	logger.RegisterSink(lager.NewWriterSink(os.Stdout, laggerLogLevel))

	return logger
}

func main() {
	flag.Parse()

	config, err := LoadConfig(configFilePath)
	if err != nil {
		log.Fatalf("Error loading config file: %s", err)
	}

	logger := buildLogger(config.LogLevel)

	awsConfig := aws.NewConfig().WithRegion(config.ElastiCacheConfig.Region)
	awsSession := session.New(awsConfig)

	iamsvc := iam.New(awsSession)
	elasticachesvc := elasticache.New(awsSession)
	cacheCluster := awselasticache.NewElastiCacheCluster(config.ElastiCacheConfig.Region, iamsvc, elasticachesvc, logger)

	serviceBroker := broker.New(config.ElastiCacheConfig, cacheCluster, logger)

	credentials := brokerapi.BrokerCredentials{
		Username: config.Username,
		Password: config.Password,
	}

	brokerAPI := brokerapi.New(serviceBroker, logger, credentials)
	http.Handle("/", brokerAPI)

	fmt.Println("ElastiCache Service Broker started on port " + port + "...")
	http.ListenAndServe(":"+port, nil)
}
