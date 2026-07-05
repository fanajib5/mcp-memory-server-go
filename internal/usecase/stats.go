package usecase

import (
	"context"

	"mcp-memory-server/internal/entity"
	"mcp-memory-server/internal/repository"
)

// StatsUseCase scopes UI read queries to a project (blank -> "default").
type StatsUseCase struct {
	repo repository.StatsRepository
}

func NewStatsUseCase(repo repository.StatsRepository) *StatsUseCase {
	return &StatsUseCase{repo: repo}
}

func (u *StatsUseCase) GetEntityDetail(ctx context.Context, project, name string) (*entity.EntityDetail, error) {
	return u.repo.GetEntityDetail(ctx, defaultProject(project), name)
}

func (u *StatsUseCase) ListEntities(ctx context.Context, project, typeFilter, query string, limit int) ([]entity.EntitySummary, error) {
	return u.repo.ListEntities(ctx, defaultProject(project), typeFilter, query, limit)
}

func (u *StatsUseCase) DashboardMetrics(ctx context.Context, project string) (*entity.Metrics, error) {
	return u.repo.DashboardMetrics(ctx, defaultProject(project))
}

func (u *StatsUseCase) GraphData(ctx context.Context, project string) (*entity.GraphPayload, error) {
	return u.repo.GraphData(ctx, defaultProject(project))
}

func (u *StatsUseCase) ObservationByID(ctx context.Context, project string, id int) (string, string, error) {
	return u.repo.ObservationByID(ctx, defaultProject(project), id)
}
