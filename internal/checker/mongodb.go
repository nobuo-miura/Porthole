package checker

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type MongoDBChecker struct{}

func (c *MongoDBChecker) Check(ctx context.Context, req CheckRequest) CheckResult {
	return Run(ctx, req, func(ctx context.Context) (string, error) {
		opts, err := mongoOptions(req)
		if err != nil {
			return "", err
		}

		client, err := mongo.Connect(opts)
		if err != nil {
			return "", err
		}
		defer client.Disconnect(ctx)

		if err := client.Ping(ctx, nil); err != nil {
			return "", err
		}

		// bson.M へデコードして型アサーションで辿ると、入れ子のドキュメントが
		// bson.D として来た場合に静かに失敗し、認証済みでも "MongoDB connected"
		// のままになる。型付き構造体なら形に依存せず取り出せる。
		var status struct {
			AuthInfo struct {
				AuthenticatedUsers []struct {
					User string `bson:"user"`
					DB   string `bson:"db"`
				} `bson:"authenticatedUsers"`
			} `bson:"authInfo"`
		}

		if err := client.Database("admin").
			RunCommand(ctx, bson.D{{Key: "connectionStatus", Value: 1}}).
			Decode(&status); err != nil {
			// 到達はできているので成功扱い。付加情報が取れないだけ。
			return "MongoDB connected", nil
		}

		if users := status.AuthInfo.AuthenticatedUsers; len(users) > 0 {
			return fmt.Sprintf("MongoDB connected | authenticated as %s@%s",
				users[0].User, users[0].DB), nil
		}

		// 認証情報を渡していない、または匿名接続。認証済みだと誤解させない。
		return "MongoDB connected (no authenticated user)", nil
	})
}

// mongoOptions は接続オプションを組み立てる。
//
// 以前は fmt.Sprintf("mongodb://%s:%s@%s:%d", user, pass, host, port) で
// 認証情報を URI に埋め込んでいたため、パスワードに "@" や "/" や ":" が
// 含まれると URI の区切りと誤認され、接続先やユーザ名がすり替わっていた。
// 認証情報は URI に入れず SetAuth で渡す。
//
// あわせて、以前は username と password が「両方」揃っていないと認証情報を
// 付けていなかったため、パスワード無しのユーザや入力漏れが黙って匿名接続に
// なっていた。username があれば認証を試みるようにしている。
func mongoOptions(req CheckRequest) (*options.ClientOptions, error) {
	// uri 経路でも ssl_mode を検証する（旧実装は uri があると迂回していた）。
	if err := checkSSLModeAgainstURI(req, mongoSSLModes...); err != nil {
		return nil, err
	}

	if req.URI != "" {
		return options.Client().ApplyURI(req.URI), nil
	}

	opts := options.Client().ApplyURI("mongodb://" + req.Addr(27017))

	if req.Username != "" {
		opts.SetAuth(options.Credential{
			Username: req.Username,
			Password: req.Password,
			// パスワードが空でも「空のパスワードを設定した」と明示する。
			// これを立てないとドライバがパスワード未設定として扱う。
			PasswordSet: true,
		})
	}

	// 以前は switch の default で未知の値を無視していたため、verify-full や
	// verify-ca を指定しても、タイプミスでも、黙って TLS 無しで接続していた。
	mode, err := resolveSSLMode(req.SSLMode, mongoSSLModes...)
	if err != nil {
		return nil, err
	}

	tlsConfig, err := tlsConfigForMode(mode, nil)
	if err != nil {
		return nil, err
	}
	if tlsConfig != nil {
		opts.SetTLSConfig(tlsConfig)
	}

	return opts, nil
}
