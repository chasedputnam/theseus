package db

import (
	"database/sql"
	"fmt"
	"time"
)

func (db *DB) CreateScheduledTask(t *ScheduledTask) error {
	_, err := db.Exec(`INSERT INTO scheduled_tasks
		(id,owner,name,prompt,task_type,action,schedule,scheduled_time,scheduled_day,
		 scheduled_date,trigger_type,trigger_event,trigger_count,trigger_counter,
		 next_run,last_run,status,output_target,session_id,model,endpoint_url,run_count,
		 cron_expression,then_task_id,webhook_token,crew_member_id,max_steps,
		 email_results,notifications_enabled,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Owner, t.Name, t.Prompt, t.TaskType, t.Action, t.Schedule,
		t.ScheduledTime, t.ScheduledDay, t.ScheduledDate, t.TriggerType,
		t.TriggerEvent, t.TriggerCount, t.TriggerCounter, t.NextRun, t.LastRun,
		t.Status, t.OutputTarget, t.SessionID, t.Model, t.EndpointURL, t.RunCount,
		t.CronExpression, t.ThenTaskID, t.WebhookToken, t.CrewMemberID, t.MaxSteps,
		boolInt(t.EmailResults), boolInt(t.NotificationsEnabled), now(), now(),
	)
	return err
}

func (db *DB) GetScheduledTask(id string) (*ScheduledTask, error) {
	row := db.QueryRow(`SELECT id,owner,name,prompt,task_type,action,schedule,scheduled_time,
		scheduled_day,scheduled_date,trigger_type,trigger_event,trigger_count,trigger_counter,
		next_run,last_run,status,output_target,session_id,model,endpoint_url,run_count,
		cron_expression,then_task_id,webhook_token,crew_member_id,max_steps,
		email_results,notifications_enabled,created_at,updated_at
		FROM scheduled_tasks WHERE id=?`, id)
	return scanTask(row)
}

func (db *DB) ListScheduledTasks(owner string) ([]*ScheduledTask, error) {
	q := `SELECT id,owner,name,prompt,task_type,action,schedule,scheduled_time,
		scheduled_day,scheduled_date,trigger_type,trigger_event,trigger_count,trigger_counter,
		next_run,last_run,status,output_target,session_id,model,endpoint_url,run_count,
		cron_expression,then_task_id,webhook_token,crew_member_id,max_steps,
		email_results,notifications_enabled,created_at,updated_at
		FROM scheduled_tasks`
	args := []any{}
	if owner != "" {
		q += " WHERE owner=?"
		args = append(args, owner)
	}
	q += " ORDER BY created_at DESC"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []*ScheduledTask
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (db *DB) UpdateScheduledTask(t *ScheduledTask) error {
	_, err := db.Exec(`UPDATE scheduled_tasks SET
		name=?,prompt=?,task_type=?,action=?,schedule=?,scheduled_time=?,scheduled_day=?,
		scheduled_date=?,trigger_type=?,trigger_event=?,trigger_count=?,next_run=?,last_run=?,
		status=?,model=?,endpoint_url=?,run_count=?,cron_expression=?,then_task_id=?,
		max_steps=?,email_results=?,notifications_enabled=?,updated_at=? WHERE id=?`,
		t.Name, t.Prompt, t.TaskType, t.Action, t.Schedule, t.ScheduledTime, t.ScheduledDay,
		t.ScheduledDate, t.TriggerType, t.TriggerEvent, t.TriggerCount, t.NextRun, t.LastRun,
		t.Status, t.Model, t.EndpointURL, t.RunCount, t.CronExpression, t.ThenTaskID,
		t.MaxSteps, boolInt(t.EmailResults), boolInt(t.NotificationsEnabled), now(), t.ID,
	)
	return err
}

func (db *DB) DeleteScheduledTask(id string) error {
	_, err := db.Exec(`DELETE FROM scheduled_tasks WHERE id=?`, id)
	return err
}

func (db *DB) CreateTaskRun(r *TaskRun) error {
	_, err := db.Exec(`INSERT INTO task_runs (id,task_id,started_at,status,model)
		VALUES (?,?,?,?,?)`,
		r.ID, r.TaskID, r.StartedAt, r.Status, r.Model,
	)
	return err
}

func (db *DB) UpdateTaskRun(r *TaskRun) error {
	_, err := db.Exec(`UPDATE task_runs SET
		finished_at=?,status=?,result=?,error=?,tokens_used=?,steps=? WHERE id=?`,
		r.FinishedAt, r.Status, r.Result, r.Error, r.TokensUsed, r.Steps, r.ID,
	)
	return err
}

func (db *DB) ListTaskRuns(taskID string, limit int) ([]*TaskRun, error) {
	q := `SELECT id,task_id,started_at,finished_at,status,result,error,tokens_used,steps,model
		FROM task_runs WHERE task_id=? ORDER BY started_at DESC`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := db.Query(q, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []*TaskRun
	for rows.Next() {
		r := &TaskRun{}
		var startedAt string
		if err := rows.Scan(&r.ID, &r.TaskID, &startedAt, &r.FinishedAt, &r.Status,
			&r.Result, &r.Error, &r.TokensUsed, &r.Steps, &r.Model); err != nil {
			return nil, err
		}
		r.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// ListDueTasks returns active tasks whose next_run is in the past.
func (db *DB) ListDueTasks() ([]*ScheduledTask, error) {
	rows, err := db.Query(`SELECT id,owner,name,prompt,task_type,action,schedule,scheduled_time,
		scheduled_day,scheduled_date,trigger_type,trigger_event,trigger_count,trigger_counter,
		next_run,last_run,status,output_target,session_id,model,endpoint_url,run_count,
		cron_expression,then_task_id,webhook_token,crew_member_id,max_steps,
		email_results,notifications_enabled,created_at,updated_at
		FROM scheduled_tasks WHERE status='active' AND next_run <= ? AND trigger_type='schedule'`,
		now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []*ScheduledTask
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func scanTask(row scanner) (*ScheduledTask, error) {
	t := &ScheduledTask{}
	var emailResults, notificationsEnabled int
	var createdAt, updatedAt string
	err := row.Scan(
		&t.ID, &t.Owner, &t.Name, &t.Prompt, &t.TaskType, &t.Action, &t.Schedule,
		&t.ScheduledTime, &t.ScheduledDay, &t.ScheduledDate, &t.TriggerType,
		&t.TriggerEvent, &t.TriggerCount, &t.TriggerCounter, &t.NextRun, &t.LastRun,
		&t.Status, &t.OutputTarget, &t.SessionID, &t.Model, &t.EndpointURL, &t.RunCount,
		&t.CronExpression, &t.ThenTaskID, &t.WebhookToken, &t.CrewMemberID, &t.MaxSteps,
		&emailResults, &notificationsEnabled, &createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("task not found")
		}
		return nil, err
	}
	t.EmailResults = emailResults != 0
	t.NotificationsEnabled = notificationsEnabled != 0
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return t, nil
}
