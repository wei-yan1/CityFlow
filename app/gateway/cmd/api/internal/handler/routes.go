package handler

import (
	"net/http"
	"time"

	"cityflow/app/gateway/cmd/api/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

// RegisterHandlers is the single route table for the public edge. It mirrors
// gateway.api exactly; cookie/page handling is why this service keeps a
// hand-maintained table instead of overwriting generated handlers on every
// goctl run. The .api contract remains the source of truth and is validated in
// CI/development with `goctl api validate`.
func RegisterHandlers(server *rest.Server, serverCtx *svc.ServiceContext) {
	timeout := time.Duration(serverCtx.Config.RequestTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	options := []rest.RouteOption{
		rest.WithTimeout(timeout),
		rest.WithMaxBytes(1 << 20),
	}
	server.AddRoutes([]rest.Route{
		{Method: http.MethodGet, Path: "/healthz", Handler: http.HandlerFunc(Health)},
		{Method: http.MethodGet, Path: "/readyz", Handler: Ready(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/auth/login", Handler: Login(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/auth/refresh", Handler: Refresh(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/auth/logout", Handler: Logout(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/auth/register", Handler: Register(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/auth/password-reset/request", Handler: RequestPasswordReset(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/auth/password-reset/confirm", Handler: ResetPassword(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/rider-registrations", Handler: SubmitRiderRegistration(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/payment-notify/:provider", Handler: PaymentNotify(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/map/config", Handler: MapConfig(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/map/merchants", Handler: MapMerchants(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/map/search", Handler: MapSearch(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/map/nearby", Handler: MapNearby(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/map/riding", Handler: MapRiding(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/map/city", Handler: MapCity(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/map/reverse", Handler: MapReverse(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/map/reverse-address", Handler: MapReverse(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/map/district", Handler: MapDistrict(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/merchants/hot", Handler: ListHotMerchants(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/places/:source/:id", Handler: PlaceDetail(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/reviews/:id/comments", Handler: ListPlaceComments(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/reviews/:id/comments/:rootId/thread", Handler: ListPlaceCommentThread(serverCtx)},
		{Method: http.MethodGet, Path: "/uploads/:file", Handler: UploadedFile(serverCtx)},
	}, options...)
	// WebSocket is not a REST route and authenticates through the
	// Sec-WebSocket-Protocol token. Tracking owns the stream and its
	// task/rider authorization; Gateway only forwards the upgrade.
	server.AddRoutes([]rest.Route{
		{Method: http.MethodGet, Path: "/api/v1/tasks/:id/location/stream", Handler: TrackingStream(serverCtx)},
	})
	server.AddRoutes(rest.WithMiddlewares([]rest.Middleware{Auth(serverCtx)}, []rest.Route{
		{Method: http.MethodGet, Path: "/api/v1/auth/me", Handler: Me(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/users/me", Handler: UserProfile(serverCtx)},
		{Method: http.MethodPut, Path: "/api/v1/users/me", Handler: UpdateUserProfile(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/platform/users", Handler: ListUsers(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/platform/users", Handler: CreateUser(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/platform/users/:id", Handler: GetUser(serverCtx)},
		{Method: http.MethodPut, Path: "/api/v1/platform/users/:id", Handler: UpdateUser(serverCtx)},
		{Method: http.MethodDelete, Path: "/api/v1/platform/users/:id", Handler: DisableUser(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/platform/rider-registrations", Handler: ListRiderRegistrations(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/platform/rider-registrations/:id/approve", Handler: ApproveRiderRegistration(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/platform/rider-registrations/:id/reject", Handler: RejectRiderRegistration(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/tasks", Handler: ListTasks(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/notifications", Handler: ListNotifications(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/notifications/unread", Handler: UnreadNotifications(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/notifications/read", Handler: MarkNotificationsRead(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/tasks", Handler: CreateTask(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/tasks/:id", Handler: GetTask(serverCtx)},
		{Method: http.MethodPut, Path: "/api/v1/tasks/:id", Handler: UpdateTask(serverCtx)},
		{Method: http.MethodPatch, Path: "/api/v1/tasks/:id", Handler: UpdateTask(serverCtx)},
		{Method: http.MethodDelete, Path: "/api/v1/tasks/:id", Handler: CancelTask(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/tasks/:id/claim", Handler: ClaimTask(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/tasks/:id/status", Handler: UpdateStatus(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/tasks/:id/pickups/:index", Handler: AdvancePickup(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/tasks/:id/payment", Handler: GetTaskPayment(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/tasks/:id/payment", Handler: CreateTaskPayment(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/payments/:id/mock-pay", Handler: MockPay(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/payments/:id/sync", Handler: SyncPayment(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/payments/:id/refund", Handler: RefundPayment(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/coupons", Handler: ListPromotionCoupons(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/coupons/mine", Handler: ListMyPromotionCoupons(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/coupons/:id/claim", Handler: ClaimPromotionCoupon(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/coupons/redeem", Handler: RedeemPromotionCoupon(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/platform/coupons/:id/redeem-codes/export", Handler: ExportRedeemCodes(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/platform/coupons/:id/redeem-codes", Handler: ListRedeemCodes(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/platform/coupons/:id/redeem-codes", Handler: GenerateRedeemCodes(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/platform/redeem-codes/export", Handler: ExportAllRedeemCodes(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/platform/redeem-codes", Handler: ListAllRedeemCodes(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/platform/redeem-codes/:id/disable", Handler: DisableRedeemCode(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/coupons/task/:taskId", Handler: ListTaskCoupons(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/riders/me/settlements", Handler: RiderSettlements(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/riders/me/payout-account", Handler: GetPayoutAccount(serverCtx)},
		{Method: http.MethodPut, Path: "/api/v1/riders/me/payout-account", Handler: SavePayoutAccount(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/riders/me", Handler: RiderMe(serverCtx)},
		{Method: http.MethodPut, Path: "/api/v1/riders/me", Handler: UpdateRiderMe(serverCtx)},
		{Method: http.MethodPut, Path: "/api/v1/riders/me/status", Handler: UpdateRiderStatus(serverCtx)},
		{Method: http.MethodDelete, Path: "/api/v1/riders/me", Handler: ArchiveRiderMe(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/riders/me/location", Handler: ReportLocation(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/tasks/:id/location", Handler: TaskLocation(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/tasks/:id/history", Handler: TaskLocationHistory(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/platform/rider-locations", Handler: NearbyRiders(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/tasks/:id/rider", Handler: TaskRider(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/rider-profiles/:id", Handler: PublicRider(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/tasks/:id/rider/reviews", Handler: CreateRiderReview(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/tasks/:id/rider/tip", Handler: CreateRiderTip(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/tasks/:id/call-rider", Handler: CallRider(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/tasks/:id/incidents/:incidentId/resolve", Handler: ResolveTaskIncident(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/platform/task-incidents", Handler: ListTaskIncidents(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/platform/riders", Handler: ListRiders(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/platform/riders", Handler: CreateRider(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/platform/riders/:id", Handler: GetRider(serverCtx)},
		{Method: http.MethodPut, Path: "/api/v1/platform/riders/:id", Handler: UpdateRider(serverCtx)},
		{Method: http.MethodDelete, Path: "/api/v1/platform/riders/:id", Handler: ArchiveRider(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/platform/rider-reports", Handler: ListRiderReports(serverCtx)},
		{Method: http.MethodPatch, Path: "/api/v1/platform/rider-reports/:id", Handler: ResolveRiderReport(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/merchants/:id/likes", Handler: SetMerchantLike(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/merchants/me", Handler: MerchantMe(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/merchants/me", Handler: CreateMerchantMe(serverCtx)},
		{Method: http.MethodPut, Path: "/api/v1/merchants/me", Handler: UpdateMerchantMe(serverCtx)},
		{Method: http.MethodDelete, Path: "/api/v1/merchants/me", Handler: DeleteMerchantMe(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/merchants/me/media", Handler: MerchantMedia(serverCtx)},
		{Method: http.MethodPut, Path: "/api/v1/merchants/me/media", Handler: SaveMerchantMedia(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/merchants/me/media/upload", Handler: UploadMerchantImage(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/places/:source/:id/reviews", Handler: CreatePlaceReview(serverCtx)},
		{Method: http.MethodDelete, Path: "/api/v1/reviews/:id", Handler: DeletePlaceReview(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/reviews/:id/comments", Handler: CreatePlaceComment(serverCtx)},
		{Method: http.MethodDelete, Path: "/api/v1/reviews/:id/comments/:commentId", Handler: HidePlaceComment(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/rider-feedback/:id/comments", Handler: CreateRiderComment(serverCtx)},
		{Method: http.MethodDelete, Path: "/api/v1/rider-feedback/:id/comments/:commentId", Handler: HideRiderComment(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/rider-feedback/:id/comments", Handler: ListRiderComments(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/rider-feedback/:id/comments/:rootId/thread", Handler: ListRiderCommentThread(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/reactions", Handler: SetReaction(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/reactions/state", Handler: GetReactionState(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/reactions/mine", Handler: ListMineReactions(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/tasks/:id/reviews/target", Handler: TaskReviewTarget(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/tasks/:id/merchant-review-target", Handler: TaskReviewTarget(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/tasks/:id/reviews", Handler: CreateTaskMerchantReview(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/tasks/:id/merchant-reviews", Handler: CreateTaskMerchantReview(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/merchants/me/reviews", Handler: ListMyMerchantReviews(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/platform/reviews", Handler: ListPlaceReviewsAdmin(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/platform/reviews/:id/reveal", Handler: RevealPlaceReviewIdentity(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/platform/merchants", Handler: ListMerchants(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/platform/merchants", Handler: CreateMerchant(serverCtx)},
		{Method: http.MethodGet, Path: "/api/v1/platform/merchants/:id", Handler: GetMerchant(serverCtx)},
		{Method: http.MethodPut, Path: "/api/v1/platform/merchants/:id", Handler: UpdateMerchant(serverCtx)},
		{Method: http.MethodDelete, Path: "/api/v1/platform/merchants/:id", Handler: DeleteMerchant(serverCtx)},
	}...), options...)
	// Feedback images and incident evidence arrive as data URLs. Keep ordinary
	// JSON routes on the 1 MiB edge budget, but allow the evidence-bearing
	// routes the same 12 MiB bound as the previous HTTP handlers.
	server.AddRoutes(rest.WithMiddlewares([]rest.Middleware{Auth(serverCtx)}, []rest.Route{
		{Method: http.MethodPost, Path: "/api/v1/tasks/:id/rider/reports", Handler: CreateRiderReport(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/tasks/:id/incidents", Handler: CreateTaskIncident(serverCtx)},
		{Method: http.MethodPost, Path: "/api/v1/uploads", Handler: UploadMerchantImage(serverCtx)},
	}...), rest.WithMaxBytes(12<<20), rest.WithTimeout(timeout))
}
