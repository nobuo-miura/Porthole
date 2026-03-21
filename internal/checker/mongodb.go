package checker

import (
	"context"
	"crypto/tls"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type MongoDBChecker struct{}

func (c *MongoDBChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		var opts *options.ClientOptions

		if req.URI != "" {
			opts = options.Client().ApplyURI(req.URI)
		} else {
			host := req.Host
			port := req.Port
			if port == 0 {
				port = 27017
			}
			var uri string
			if req.Username != "" && req.Password != "" {
				uri = fmt.Sprintf("mongodb://%s:%s@%s:%d", req.Username, req.Password, host, port)
			} else {
				uri = fmt.Sprintf("mongodb://%s:%d", host, port)
			}
			opts = options.Client().ApplyURI(uri)

			switch req.SSLMode {
			case "require":
				opts.SetTLSConfig(&tls.Config{InsecureSkipVerify: false})
			case "skip-verify":
				opts.SetTLSConfig(&tls.Config{InsecureSkipVerify: true})
			}
		}

		client, err := mongo.Connect(opts)
		if err != nil {
			return "", err
		}
		defer client.Disconnect(ctx)

		if err := client.Ping(ctx, nil); err != nil {
			return "", err
		}

		var status bson.M
		err = client.Database("admin").RunCommand(ctx, bson.D{{Key: "connectionStatus", Value: 1}}).Decode(&status)
		if err != nil {
			return "MongoDB connected", nil
		}

		detail := "MongoDB connected"
		if authInfo, ok := status["authInfo"].(bson.M); ok {
			if users, ok := authInfo["authenticatedUsers"].(bson.A); ok && len(users) > 0 {
				if user, ok := users[0].(bson.M); ok {
					detail = fmt.Sprintf("MongoDB connected | authenticated as %v@%v", user["user"], user["db"])
				}
			}
		}
		return detail, nil
	})
}
