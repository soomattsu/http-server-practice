package platform

import (
	"fmt"
	"log"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitMySQL(cfg *Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"%v:%v@tcp(%v:%v)/%v?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.MySQLUser,
		cfg.MySQLPassword,
		cfg.MySQLHost,
		cfg.MySQLPort,
		cfg.MySQLDatabase,
	)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		TranslateError: true,
		Logger:         logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open MySQL session: %v", err)
	}

	/*
		sql.DB
		- GoプロセスがDBに対して確保するコネクションプールのハンドル（間接的に利用するためのインターフェース）
		- デフォルトではlazy connectionで、sql.Open()しただけではコネクションは1本も張られない
		- 複数goroutineからの並行アクセスに対して安全（所謂スレッドセーフ）
	*/
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sqlDB from gorm: %v", err)
	}
	/*
		コネクション(DB)
		- クライアントプロセスとDBの間のTCP接続と、DBが各々に割り当てたセッション（認証コンテキスト、メモリ、子プロセス/スレッドなど）をまとめて呼称したもの
			- つまり、クライアントプロセス<->DB間のstatefulな通信経路
		- クライアントの単位はプロセスであり、ホスト/スレッド/goroutineではない
			1. DBコネクションの実体はTCPコネクションであり、TCPコネクションの実体は個別のソケット（src-IP, src-port(ephemeral), dest-IP, dest-portの4要素の一意な組）
			2. ソケットは、OSによって"プロセス単位"に割り振られる（プロセスは、複数のソケットを同時に保持・利用できる）
			3. よって、DBコネクションにおけるクライアントの単位も（他のTCP上の通信同様）プロセスになる
		- **「宛先DBサーバ（dest IP/Port）＆認証コンテキスト（user/pass/database/etc...）等が同じなら、クライアントプロセスから見て各コネクションは交換可能」と言える**
			- 要約：クライアントは、DSNが同じ複数のコネクションを交換可能に扱える
			- これがコネクションプールのfeasibilityに直結する

		コネクションプール
		- 定義：クライアントプロセスが、確立済みの1つ以上の交換可能なDBコネクションを、メモリ上に保持して使い回す仕組み
		- 意義
			1. 接続時のI/Oオーバーヘッドを避けるため（DBコネクションの確立は、クエリ実行そのものよりコストが高くなりがち）
			2. プロセス内のgoroutineやスレッドからクエリを並行実行する経路を確保するため（主要なRDBMSの仕様・実装では、1つのコネクションでは1クエリずつ順番にしか処理できない）
		- プール内のコネクションは、2つの状態に大別される
			- in-use: 使用中。クエリ実行中で、処理が完了するとpoolに"返却"されてidleになる or closeされる
			- idle: 待機中。pool上でクエリ処理に割り当てられるのを待っている
		- 補足：RDBMS側は「サーバ/ユーザー/ロール/DBなどの単位で同時接続上限を設定しておき、超過したら接続を拒否する」シンプルな仕組みが多い

		database/sqlによるGoプロセスのコネクション系設定値
		- MaxOpenConns
			- 同時にopenできるコネクション数の上限（デフォルト無制限）
			- MaxOpenConns = in-use + idle + 未使用枠
			- 上限に達すると、コネクションに空きが出るまでクエリ実行がブロックされる
		- MaxIdleConns
			- プール上にidle状態でキープしておけるコネクション数の上限（デフォルト2）
			- メモリが許すなら、MaxOpenConnsと同値 or 充分大きい値にしておく
				- OpenConnsだけ高い状態で並行クエリを実行しても、IdleConns超過分はすぐにcloseされてしまい、次のクエリ実行でDB接続オーバーヘッドが生じる
		- ConnMaxLifetime
			- コネクションの有効期間・寿命
			- コネクションがopenされてからの累積時間で判定
			- expiredなコネクションは遅延close（cleanerの定期実行時や、reuseを試みて参照された際にcloseされる）
			- デフォルトでは上限無し（新しいDBインスタンスに接続すべきタイミング(fail-over,blue/green)で、古い接続を使い続けたりしてしまう）
		- ConnMaxIdleTime
			- コネクションがidle状態でプール上に存在できる時間の上限
			- プール上に生成・返却されるたびにカウントリセット
			- expiredなコネクションは遅延close（cleanerの定期実行でclose）
			- デフォルトでは上限無し
		コネクションプールの利用状況は、*sql.DBのStats()メソッドから取得できる
	*/
	sqlDB.SetMaxOpenConns(20)

	log.Println("Success to open MySQL session!")
	return db, nil
}
