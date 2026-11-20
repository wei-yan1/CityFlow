package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"cityflow/internal/application/repository"
	"cityflow/internal/domain"
	"cityflow/internal/domain/auth"
	"cityflow/internal/domain/feedback"
	"cityflow/internal/domain/rider"
	"cityflow/internal/domain/task"
)

type RiderFeedbackService struct {
	feedback       repository.RiderFeedback
	tasks          taskReader
	riders         riderReader
	users          accountReader
	Reactions      ReactionReader
	ReactionTarget string
	now            func() time.Time
}

func NewRiderFeedbackService(items repository.RiderFeedback, tasks taskReader, riders riderReader, users accountReader) *RiderFeedbackService {
	return &RiderFeedbackService{feedback: items, tasks: tasks, riders: riders, users: users, now: func() time.Time { return time.Now().UTC() }}
}

type PublicRiderFeedback struct {
	ID          string `json:"id"`
	AuthorLabel string `json:"author_label"`
	Rating      int    `json:"rating,omitempty"`
	AmountFen   int64  `json:"amount_fen,omitempty"`
	Comment     string `json:"comment,omitempty"`
	Date        string `json:"date"`
	Likes       int    `json:"likes"`
	Liked       bool   `json:"liked"`
}

type PublicRider struct {
	ID             string                `json:"id"`
	DisplayName    string                `json:"display_name"`
	AvatarURL      string                `json:"avatar_url,omitempty"`
	VehicleType    string                `json:"vehicle_type"`
	ServiceArea    string                `json:"service_area,omitempty"`
	Skills         []string              `json:"skills,omitempty"`
	Status         string                `json:"status"`
	Rating         float64               `json:"rating"`
	CompletedTasks int                   `json:"completed_tasks"`
	ReviewCount    int                   `json:"review_count"`
	TipTotalFen    int64                 `json:"tip_total_fen"`
	Reviews        []PublicRiderFeedback `json:"reviews"`
	Tips           []PublicRiderFeedback `json:"tips"`
}

type TaskRiderView struct {
	TaskID    string           `json:"task_id"`
	Status    string           `json:"status"`
	Rider     *PublicRider     `json:"rider"`
	CanReview bool             `json:"can_review"`
	CanTip    bool             `json:"can_tip"`
	CanReport bool             `json:"can_report"`
	CanCall   bool             `json:"can_call"`
	MyReview  *feedback.Record `json:"my_review,omitempty"`
}

func (s *RiderFeedbackService) PublicRider(ctx context.Context, riderID, userID string) (*PublicRider, error) {
	profile, err := s.riders.GetRider(ctx, riderID)
	if err != nil {
		return nil, err
	}
	items, err := s.feedback.ListRiderFeedback(ctx, riderID, []string{feedback.KindReview, feedback.KindTip}, 100, 0)
	if err != nil {
		return nil, err
	}
	name, avatar := publicRiderName(profile.DisplayName, profile.ID), ""
	if account, err := s.users.GetUser(ctx, profile.UserID); err == nil && account != nil {
		// The account avatar is presentable; the account holder's real name is
		// not, so it never stands in for a missing rider display name.
		avatar = account.AvatarURL
	}
	result := &PublicRider{
		ID: profile.ID, DisplayName: name, AvatarURL: avatar, VehicleType: profile.VehicleType,
		ServiceArea: profile.ServiceArea, Skills: profile.Skills, Status: profile.Status,
		Rating: profile.Rating, CompletedTasks: profile.CompletedTasks, Reviews: []PublicRiderFeedback{}, Tips: []PublicRiderFeedback{},
	}
	var ratingTotal float64
	for _, item := range items {
		public := publicRiderFeedback(item)
		switch item.Kind {
		case feedback.KindReview:
			result.Reviews = append(result.Reviews, public)
			result.ReviewCount++
			ratingTotal += float64(item.Rating)
		case feedback.KindTip:
			result.Tips = append(result.Tips, public)
			result.TipTotalFen += item.AmountFen
		}
	}
	if result.ReviewCount > 0 {
		result.Rating = ratingTotal / float64(result.ReviewCount)
	}
	s.overlayReviewReactions(ctx, result.Reviews, userID)
	return result, nil
}

// overlayReviewReactions fills rider-review likes from Reaction. Rider
// reviews have no like_count projection of their own; the count is display
// state only and degrades to zero when Reaction is unavailable.
func (s *RiderFeedbackService) overlayReviewReactions(ctx context.Context, reviews []PublicRiderFeedback, userID string) {
	if s.Reactions == nil || s.ReactionTarget == "" || len(reviews) == 0 {
		return
	}
	ids := make([]string, 0, len(reviews))
	for i := range reviews {
		ids = append(ids, reviews[i].ID)
	}
	states, err := s.Reactions.ReactionStates(ctx, s.ReactionTarget, ids, "LIKE", userID)
	if err != nil {
		LogDegradation("read rider review reaction projection", err)
		return
	}
	for i := range reviews {
		state, ok := states[reviews[i].ID]
		if !ok {
			continue
		}
		reviews[i].Likes = int(state.Count)
		reviews[i].Liked = state.Active
	}
}

// publicRiderFeedback forces every public rider review and tip to anonymous.
// The rider never receives user_id, task_id or exact feedback time.
func publicRiderFeedback(item feedback.Record) PublicRiderFeedback {
	return PublicRiderFeedback{
		ID: item.ID, AuthorLabel: "匿名用户", Rating: item.Rating,
		AmountFen: item.AmountFen, Comment: item.Comment, Date: reviewDate(item.CreatedAt),
	}
}

// publicRiderName keeps the account holder's real name out of the public
// profile. A rider who has not chosen a display name gets a stable anonymous
// label derived from the profile ID instead.
func publicRiderName(displayName, riderID string) string {
	if name := strings.TrimSpace(displayName); name != "" {
		return name
	}
	suffix := riderID
	if len(suffix) > 4 {
		suffix = suffix[len(suffix)-4:]
	}
	if suffix == "" {
		return "骑手"
	}
	return "骑手·" + suffix
}

func (s *RiderFeedbackService) TaskRider(ctx context.Context, taskID, subject, role string) (*TaskRiderView, error) {
	item, err := s.tasks.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if role != auth.RoleAdmin && item.CreatedBy != subject && item.WorkerID != subject {
		return nil, domain.ErrForbidden
	}
	if item.WorkerID == "" {
		return nil, fmt.Errorf("%w: 任务尚未接单", domain.ErrNotFound)
	}
	profile, err := s.riders.RiderForUser(ctx, item.WorkerID)
	if err != nil {
		return nil, err
	}
	riderView, err := s.PublicRider(ctx, profile.ID, subject)
	if err != nil {
		return nil, err
	}
	result := &TaskRiderView{TaskID: item.ID, Status: item.Status, Rider: riderView}
	result.CanReview = item.CreatedBy == subject && item.Status == task.StatusCompleted
	result.CanTip = item.CreatedBy == subject
	result.CanReport = item.CreatedBy == subject
	result.CanCall = item.CreatedBy == subject
	if result.CanReview {
		items, listErr := s.feedback.ListRiderFeedback(ctx, profile.ID, []string{feedback.KindReview}, 100, 0)
		if listErr != nil {
			return nil, listErr
		}
		for index := range items {
			if items[index].TaskID == item.ID && items[index].UserID == subject {
				review := items[index]
				result.MyReview = &review
				break
			}
		}
	}
	return result, nil
}

func (s *RiderFeedbackService) CreateReview(ctx context.Context, taskID, userID string, request feedback.ReviewRequest) (*feedback.Record, error) {
	if request.Rating < 1 || request.Rating > 5 {
		return nil, fmt.Errorf("%w: 评分必须在 1 到 5 星之间", domain.ErrInvalidRequest)
	}
	if len([]rune(strings.TrimSpace(request.Comment))) > 500 {
		return nil, fmt.Errorf("%w: 评价不能超过 500 字", domain.ErrInvalidRequest)
	}
	item, profile, err := s.taskAndRider(ctx, taskID, userID)
	if err != nil {
		return nil, err
	}
	if item.Status != task.StatusCompleted {
		return nil, fmt.Errorf("%w: 任务完成后才能评价骑手", domain.ErrConflict)
	}
	record := s.newRecord(profile.ID, item.ID, userID, feedback.KindReview)
	record.Rating = request.Rating
	record.Comment = strings.TrimSpace(request.Comment)
	record.Status = feedback.StatusPublished
	if err := s.feedback.CreateRiderFeedback(ctx, record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *RiderFeedbackService) CreateTip(ctx context.Context, taskID, userID string, request feedback.TipRequest) (*feedback.Record, error) {
	if request.AmountFen <= 0 || request.AmountFen > 100000 {
		return nil, fmt.Errorf("%w: 打赏金额需在 0.01 到 1000 元之间", domain.ErrInvalidRequest)
	}
	if len([]rune(strings.TrimSpace(request.Message))) > 100 {
		return nil, fmt.Errorf("%w: 打赏留言不能超过 100 字", domain.ErrInvalidRequest)
	}
	item, profile, err := s.taskAndRider(ctx, taskID, userID)
	if err != nil {
		return nil, err
	}
	record := s.newRecord(profile.ID, item.ID, userID, feedback.KindTip)
	record.AmountFen = request.AmountFen
	record.Comment = strings.TrimSpace(request.Message)
	record.Status = feedback.StatusRecorded
	if err := s.feedback.CreateRiderFeedback(ctx, record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *RiderFeedbackService) CreateReport(ctx context.Context, taskID, userID string, request feedback.ReportRequest) (*feedback.Record, error) {
	if strings.TrimSpace(request.Reason) == "" || len([]rune(request.Reason)) > 200 {
		return nil, fmt.Errorf("%w: 请填写举报原因，且不超过 200 字", domain.ErrInvalidRequest)
	}
	if len([]rune(request.Details)) > 1000 {
		return nil, fmt.Errorf("%w: 举报详情不能超过 1000 字", domain.ErrInvalidRequest)
	}
	if len(request.Images) > 6 {
		return nil, fmt.Errorf("%w: 最多上传 6 张图片", domain.ErrInvalidRequest)
	}
	total := 0
	for _, image := range request.Images {
		if len(image) > 3*1024*1024 {
			return nil, fmt.Errorf("%w: 单张图片不能超过 3MB", domain.ErrInvalidRequest)
		}
		if !strings.HasPrefix(image, "data:image/") && !strings.HasPrefix(image, "https://") && !strings.HasPrefix(image, "http://") {
			return nil, fmt.Errorf("%w: 图片格式不支持", domain.ErrInvalidRequest)
		}
		total += len(image)
	}
	if total > 10*1024*1024 {
		return nil, fmt.Errorf("%w: 图片总大小不能超过 10MB", domain.ErrInvalidRequest)
	}
	item, profile, err := s.taskAndRider(ctx, taskID, userID)
	if err != nil {
		return nil, err
	}
	record := s.newRecord(profile.ID, item.ID, userID, feedback.KindReport)
	record.Reason = strings.TrimSpace(request.Reason)
	record.Details = strings.TrimSpace(request.Details)
	record.Images = append([]string(nil), request.Images...)
	record.Status = feedback.StatusOpen
	if err := s.feedback.CreateRiderFeedback(ctx, record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *RiderFeedbackService) CallRider(ctx context.Context, taskID, userID string) (*feedback.Record, error) {
	item, profile, err := s.taskAndRider(ctx, taskID, userID)
	if err != nil {
		return nil, err
	}
	record := s.newRecord(profile.ID, item.ID, userID, feedback.KindCall)
	record.Comment = "用户请求联系骑手"
	record.Status = feedback.StatusRecorded
	if err := s.feedback.CreateRiderFeedback(ctx, record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *RiderFeedbackService) Reports(ctx context.Context, status string, page repository.Page) ([]feedback.Record, error) {
	items, err := s.feedback.ListRiderReports(ctx, status, page)
	if err != nil {
		return nil, err
	}
	for index := range items {
		if profile, err := s.riders.GetRider(ctx, items[index].RiderID); err == nil {
			items[index].RiderName = profile.DisplayName
		}
		if account, err := s.users.GetUser(ctx, items[index].UserID); err == nil {
			items[index].UserName = account.Name
		}
		if item, err := s.tasks.Get(ctx, items[index].TaskID); err == nil {
			items[index].TaskTitle = item.Title
		}
	}
	return items, nil
}

func (s *RiderFeedbackService) ResolveReport(ctx context.Context, id, adminID, status, note string) (*feedback.Record, error) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status != feedback.StatusResolved && status != feedback.StatusDismissed {
		return nil, fmt.Errorf("%w: 处理结果必须是 RESOLVED 或 DISMISSED", domain.ErrInvalidRequest)
	}
	if len([]rune(note)) > 500 {
		return nil, fmt.Errorf("%w: 处理说明不能超过 500 字", domain.ErrInvalidRequest)
	}
	items, err := s.feedback.ListRiderReports(ctx, "", repository.Page{Limit: 200, Offset: 0})
	if err != nil {
		return nil, err
	}
	var target *feedback.Record
	for index := range items {
		if items[index].ID == id {
			target = &items[index]
			break
		}
	}
	if target == nil {
		return nil, domain.ErrNotFound
	}
	if err := s.feedback.ResolveRiderReport(ctx, id, adminID, status, strings.TrimSpace(note)); err != nil {
		return nil, err
	}
	target.Status, target.ResolutionNote, target.ResolvedBy = status, strings.TrimSpace(note), adminID
	now := s.now()
	target.ResolvedAt, target.UpdatedAt = &now, now
	return target, nil
}

func (s *RiderFeedbackService) taskAndRider(ctx context.Context, taskID, userID string) (*task.Task, *rider.Profile, error) {
	item, err := s.tasks.Get(ctx, taskID)
	if err != nil {
		return nil, nil, err
	}
	if item.CreatedBy != userID {
		return nil, nil, domain.ErrForbidden
	}
	if item.WorkerID == "" {
		return nil, nil, fmt.Errorf("%w: 骑手尚未接单", domain.ErrConflict)
	}
	profile, err := s.riders.RiderForUser(ctx, item.WorkerID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil, fmt.Errorf("%w: 骑手资料不存在", domain.ErrNotFound)
		}
		return nil, nil, err
	}
	return item, profile, nil
}

func (s *RiderFeedbackService) newRecord(riderID, taskID, userID, kind string) feedback.Record {
	now := s.now()
	return feedback.Record{
		ID: newFeedbackID(), RiderID: riderID, TaskID: taskID, UserID: userID,
		Kind: kind, CreatedAt: now, UpdatedAt: now, Status: feedback.StatusRecorded,
	}
}

func newFeedbackID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("fb-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}
