package mysqlstore

import (
	"cityflow/internal/domain/task"
	"cityflow/internal/domain/user"
	"context"
	"os"
	"testing"
	"time"
)

func TestPaymentScaffoldMySQL(t *testing.T) {
	dsn := os.Getenv("CITYFLOW_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("dedicated MySQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	s, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.MigrateAllServices(ctx); e != nil {
		t.Fatal(e)
	}
	id, e := newUUID()
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.CreateUser(ctx, user.CreateRequest{Email: id + "@example.test", Password: "integration-test-password", Name: "quote", Role: "USER"})
	if e != nil {
		t.Fatal(e)
	}
	defer s.orm.Exec("DELETE FROM users WHERE id=?", u.ID)
	amount := int64(1234)
	item, e := s.Create(ctx, task.CreateRequest{Title: "报价", Type: "DELIVERY", Pickup: "A", Destination: "B", QuotedAmountFen: &amount}, u.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer s.orm.Exec("DELETE FROM tasks WHERE id=?", item.ID)
	again, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer again.Close()
	got, e := again.Get(ctx, item.ID)
	if e != nil || got.QuotedAmountFen == nil || *got.QuotedAmountFen != amount {
		t.Fatalf("quote: %+v %v", got, e)
	}
	order, e := newUUID()
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.db.ExecContext(ctx, `INSERT INTO payment_orders(id,task_id,payer_id,original_amount_fen,amount_fen,idempotency_key) VALUES(?,?,?,?,?,?)`, order, item.ID, u.ID, amount, amount, "key-"+id)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.ExecContext(ctx, `DELETE FROM payment_orders WHERE id=?`, order)
	dup, e := newUUID()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.ExecContext(ctx, `INSERT INTO payment_orders(id,task_id,payer_id,original_amount_fen,amount_fen,idempotency_key) VALUES(?,?,?,?,?,?)`, dup, item.ID, u.ID, amount, amount, "key-"+id); e == nil {
		t.Fatal("duplicate payment key accepted")
	}
	if _, e = s.db.ExecContext(ctx, `INSERT INTO payment_orders(id,task_id,payer_id,original_amount_fen,amount_fen,idempotency_key) VALUES(?,?,?,?,?,?)`, dup, item.ID, u.ID, 0, 0, "other-"+id); e == nil {
		t.Fatal("zero payment accepted")
	}
	settlement, e := newUUID()
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.db.ExecContext(ctx, `INSERT INTO rider_settlements(id,task_id,payment_order_id,rider_id,amount_fen,idempotency_key) VALUES(?,?,?,?,?,?)`, settlement, item.ID, order, u.ID, amount, "settle-"+id)
	if e != nil {
		t.Fatal(e)
	}
	defer s.db.ExecContext(ctx, `DELETE FROM rider_settlements WHERE id=?`, settlement)
	var pStatus, sStatus string
	e = again.db.QueryRowContext(ctx, `SELECT p.status,s.status FROM payment_orders p JOIN rider_settlements s ON s.payment_order_id=p.id WHERE p.id=?`, order).Scan(&pStatus, &sStatus)
	if e != nil || pStatus != "PENDING" || sStatus != "PENDING" {
		t.Fatalf("statuses: %q %q %v", pStatus, sStatus, e)
	}
}
