package billing

// 账务集成测试。需要本地 PG，一键起：
//
//	docker run --rm --name xnc_pg_test -e POSTGRES_PASSWORD=dev -e POSTGRES_DB=xnc_test -p 55432:5432 -d postgres:15-alpine
//
// 然后：go test ./internal/billing/ -v
//
// TestMain 每次重建 public schema 并重放 init.sql + migrate_0007，
// 所以测试可以反复跑；每个用例用独立用户，互不干扰。

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JerryDtj/XNCAgent-go/internal/user"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"uuid"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn := "host=localhost user=postgres password=dev dbname=xnc_test port=55432 sslmode=disable TimeZone=Asia/Shanghai"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		log.Fatalf("连不上测试库，先执行：docker run --rm --name xnc_pg_test -e POSTGRES_PASSWORD=dev -e POSTGRES_DB=xnc_test -p 55432:5432 -d postgres:15-alpine（原始错误：%v）", err)
	}
	// 重建 schema 保证重跑幂等（migrate_0007 里的 DROP CONSTRAINT 不能重复执行）
	if err := db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public;").Error; err != nil {
		log.Fatalf("重建 schema 失败: %v", err)
	}
	for _, f := range []string{"init.sql", "migrate_0007_billing.sql"} {
		sqlBytes, err := os.ReadFile(filepath.Join("..", "..", "deploy", "postgres", f))
		if err != nil {
			log.Fatalf("读取 %s 失败: %v", f, err)
		}
		if err := db.Exec(string(sqlBytes)).Error; err != nil {
			log.Fatalf("执行 %s 失败: %v", f, err)
		}
	}
	testDB = db
	os.Exit(m.Run())
}

// seedUser 建一个独立测试用户，余额 available 分（total=available, frozen=0），返回 userID
func seedUser(t *testing.T, available int64) int64 {
	t.Helper()
	email := fmt.Sprintf("t%d@x.test", time.Now().UnixNano())
	var userID int64
	if err := testDB.Raw("INSERT INTO users (email, status) VALUES (?, 'active') RETURNING id", email).
		Scan(&userID).Error; err != nil {
		t.Fatalf("建用户失败: %v", err)
	}
	if err := testDB.Exec("INSERT INTO accounts (user_id, total, available, frozen) VALUES (?, ?, ?, 0)",
		userID, available, available).Error; err != nil {
		t.Fatalf("建账户失败: %v", err)
	}
	return userID
}

func loadAccount(t *testing.T, userID int64) user.Account {
	t.Helper()
	var acct user.Account
	if err := testDB.Where("user_id = ?", userID).First(&acct).Error; err != nil {
		t.Fatalf("查账户失败: %v", err)
	}
	return acct
}

func loadTransactions(t *testing.T, userID int64) []Transaction {
	t.Helper()
	var txs []Transaction
	if err := testDB.Where("user_id = ?", userID).Order("id").Find(&txs).Error; err != nil {
		t.Fatalf("查流水失败: %v", err)
	}
	return txs
}

// 正常预扣：余额搬移对、流水一条、快照对、流水挂得上 prehold
func TestPrehold_Normal(t *testing.T) {
	svc := NewService(testDB)
	uid := seedUser(t, 100)

	id, reused, err := svc.Prehold(context.Background(), uid, 20, "req-normal")
	if err != nil {
		t.Fatalf("预扣失败: %v", err)
	}
	if reused {
		t.Fatal("首次预扣不应复用")
	}

	acct := loadAccount(t, uid)
	if acct.Available != 80 || acct.Frozen != 20 || acct.Total != 100 {
		t.Fatalf("余额不对: available=%d frozen=%d total=%d, want 80/20/100", acct.Available, acct.Frozen, acct.Total)
	}

	txs := loadTransactions(t, uid)
	if len(txs) != 1 {
		t.Fatalf("流水应有 1 条, got %d", len(txs))
	}
	tx := txs[0]
	if tx.Type != "PREHOLD" || tx.Amount != 20 {
		t.Fatalf("流水类型/金额不对: %+v", tx)
	}
	// 总余额快照:预扣不动 total,所以 before == after == 100
	if tx.BalanceBefore != 100 || tx.BalanceAfter != 100 || tx.FrozenBefore != 0 || tx.FrozenAfter != 20 {
		t.Fatalf("快照不对: %+v", tx)
	}
	if tx.PreholdID == nil || *tx.PreholdID != id.String() {
		t.Fatalf("流水没挂 prehold_id: %+v", tx)
	}
	if tx.SeqNo != 1 {
		t.Fatalf("首条流水 seq_no 应为 1, got %d", tx.SeqNo)
	}
}

// 相同 request_id 再来一次：复用同一个预扣，余额和流水都不再动
func TestPrehold_ReuseSameRequestID(t *testing.T) {
	svc := NewService(testDB)
	uid := seedUser(t, 100)

	id1, reused, err := svc.Prehold(context.Background(), uid, 20, "req-reuse")
	if err != nil || reused {
		t.Fatalf("首次预扣异常: reused=%v err=%v", reused, err)
	}

	id2, reused2, err := svc.Prehold(context.Background(), uid, 20, "req-reuse")
	if err != nil {
		t.Fatalf("复用调用报错: %v", err)
	}
	if !reused2 {
		t.Fatal("相同 request_id 第二次应复用")
	}
	if id1 != id2 {
		t.Fatal("复用应返回同一个 prehold_id")
	}

	acct := loadAccount(t, uid)
	if acct.Available != 80 || acct.Frozen != 20 {
		t.Fatalf("复用不该再动余额: %+v", acct)
	}
	if n := len(loadTransactions(t, uid)); n != 1 {
		t.Fatalf("复用不该再写流水, got %d 条", n)
	}
}

// 并发双击：两个 goroutine 同一 request_id 同时打进来，
// 索引挡双扣 + 撞键转复用，最终只冻一次
func TestPrehold_ConcurrentDoubleTap(t *testing.T) {
	svc := NewService(testDB)
	uid := seedUser(t, 100)

	type result struct {
		id     uuid.UUID
		reused bool
		err    error
	}
	results := make([]result, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, reused, err := svc.Prehold(context.Background(), uid, 20, "req-dbl")
			results[i] = result{id, reused, err}
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			t.Fatalf("goroutine %d 报错: %v", i, r.err)
		}
	}
	if results[0].id != results[1].id {
		t.Fatal("双击应落到同一个 prehold")
	}
	if results[0].reused == results[1].reused {
		t.Fatalf("应恰好一个成功一个复用: %+v", results)
	}

	acct := loadAccount(t, uid)
	if acct.Frozen != 20 || acct.Available != 80 {
		t.Fatalf("双击只许冻一次: %+v", acct)
	}
	if n := len(loadTransactions(t, uid)); n != 1 {
		t.Fatalf("双击只许写一条流水, got %d", n)
	}
}

// 402 两条路径：余额归零 vs 余额够但不支持本次预扣
func TestPrehold_Errors(t *testing.T) {
	svc := NewService(testDB)

	uid0 := seedUser(t, 0)
	_, _, err := svc.Prehold(context.Background(), uid0, 20, "req-exhausted")
	if !errors.Is(err, ErrBalanceExhausted) {
		t.Fatalf("余额 0 应返回 ErrBalanceExhausted, got %v", err)
	}

	uid15 := seedUser(t, 15)
	_, _, err = svc.Prehold(context.Background(), uid15, 20, "req-insufficient")
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("应返回 ErrInsufficientBalance, got %v", err)
	}

	// 失败的请求不该留任何痕迹（预扣行、余额、流水都不动）
	if n := len(loadTransactions(t, uid0)); n != 0 {
		t.Fatalf("失败请求不该写流水, got %d 条", n)
	}
	acct := loadAccount(t, uid15)
	if acct.Available != 15 || acct.Frozen != 0 {
		t.Fatalf("失败请求不该动余额: %+v", acct)
	}
}

// ---- SettleUser 用例 ----

// 上报一条 usage → SettleUser:多退(预 10 实 1),断言余额/流水/prehold 状态/usage 置标
func TestSettleUser_Normal(t *testing.T) {
	svc := NewService(testDB)
	uid := seedUser(t, 100)

	phID, _, err := svc.Prehold(context.Background(), uid, 10, "req-s1")
	if err != nil {
		t.Fatalf("预扣失败: %v", err)
	}
	// Python 侧上报:真实样本 prompt 1906 / completion 234 → cost = 1 分
	if err := testDB.Exec(`INSERT INTO usage (prehold_id, user_id, model, prompt_tokens, completion_tokens)
		VALUES (?, ?, 'deepseek-chat', 1906, 234)`, phID.String(), uid).Error; err != nil {
		t.Fatalf("插 usage 失败: %v", err)
	}

	n, err := svc.SettleUser(context.Background(), uid, 10)
	if err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("应结算 1 条, got %d", n)
	}

	acct := loadAccount(t, uid)
	// 预 10 实 1:total 99,冻结全退,available 回到 99
	if acct.Total != 99 || acct.Available != 99 || acct.Frozen != 0 {
		t.Fatalf("余额不对: %+v, want total=99 available=99 frozen=0", acct)
	}

	txs := loadTransactions(t, uid)
	if len(txs) != 2 {
		t.Fatalf("应有 PREHOLD+SETTLE 两条流水, got %d", len(txs))
	}
	st := txs[1]
	if st.Type != "SETTLE" || st.Amount != -1 {
		t.Fatalf("SETTLE 流水类型/金额不对: %+v", st)
	}
	if st.BalanceBefore != 100 || st.BalanceAfter != 99 || st.FrozenBefore != 10 || st.FrozenAfter != 0 {
		t.Fatalf("SETTLE 快照不对: %+v", st)
	}
	if st.PreholdID == nil || *st.PreholdID != phID.String() {
		t.Fatalf("SETTLE 没挂 prehold_id: %+v", st)
	}

	var ph PreholdModel
	if err := testDB.Where("id = ?", phID).First(&ph).Error; err != nil {
		t.Fatalf("查 prehold 失败: %v", err)
	}
	if ph.Status != "settled" {
		t.Fatalf("prehold 应 settled, got %s", ph.Status)
	}
	var settled bool
	if err := testDB.Table("usage").Select("settled").Where("prehold_id = ?", phID).Scan(&settled).Error; err != nil {
		t.Fatalf("查 usage 失败: %v", err)
	}
	if !settled {
		t.Fatal("usage 应已置标")
	}
}

// 重复结算:第二轮返回 0,余额和流水纹丝不动
func TestSettleUser_ReplayZero(t *testing.T) {
	svc := NewService(testDB)
	uid := seedUser(t, 100)

	phID, _, _ := svc.Prehold(context.Background(), uid, 10, "req-s2")
	testDB.Exec(`INSERT INTO usage (prehold_id, user_id, model, prompt_tokens, completion_tokens)
		VALUES (?, ?, 'deepseek-chat', 1906, 234)`, phID.String(), uid)
	if _, err := svc.SettleUser(context.Background(), uid, 10); err != nil {
		t.Fatalf("首轮结算失败: %v", err)
	}

	n, err := svc.SettleUser(context.Background(), uid, 10)
	if err != nil {
		t.Fatalf("二轮结算不应报错: %v", err)
	}
	if n != 0 {
		t.Fatalf("二轮应返回 0, got %d", n)
	}
	acct := loadAccount(t, uid)
	if acct.Total != 99 || acct.Available != 99 || acct.Frozen != 0 {
		t.Fatalf("二轮不该动余额: %+v", acct)
	}
	if cnt := len(loadTransactions(t, uid)); cnt != 2 {
		t.Fatalf("二轮不该写流水, got %d 条", cnt)
	}
}

// Sweeper 先释放 → 结算走 RECONCILE_FIX:冻结不退,成本全额从 available 扣
func TestSettleUser_ReleasedReconcile(t *testing.T) {
	svc := NewService(testDB)
	uid := seedUser(t, 100)

	phID, _, _ := svc.Prehold(context.Background(), uid, 10, "req-s3")
	// 模拟 Sweeper 已释放(退回 10 分):available 90 → 100, frozen 10 → 0
	if err := testDB.Exec(`UPDATE accounts SET available = available + 10, frozen = frozen - 10 WHERE user_id = ?`, uid).Error; err != nil {
		t.Fatalf("模拟释放失败: %v", err)
	}
	if err := testDB.Model(&PreholdModel{}).Where("id = ?", phID).
		Update("status", "released").Error; err != nil {
		t.Fatalf("改 prehold 状态失败: %v", err)
	}
	// 真实成本 20 分(completion 100000 × 200/百万)
	testDB.Exec(`INSERT INTO usage (prehold_id, user_id, model, prompt_tokens, completion_tokens)
		VALUES (?, ?, 'deepseek-chat', 0, 100000)`, phID.String(), uid)

	n, err := svc.SettleUser(context.Background(), uid, 10)
	if err != nil {
		t.Fatalf("结算失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("应结算 1 条, got %d", n)
	}

	acct := loadAccount(t, uid)
	// total 100-20=80,available 100-20=80,frozen 保持 0
	if acct.Total != 80 || acct.Available != 80 || acct.Frozen != 0 {
		t.Fatalf("RECONCILE_FIX 余额不对: %+v, want 80/80/0", acct)
	}
	txs := loadTransactions(t, uid)
	fix := txs[len(txs)-1]
	if fix.Type != "RECONCILE_FIX" || fix.Amount != -20 {
		t.Fatalf("应有 RECONCILE_FIX 流水 -20: %+v", fix)
	}
}
