package db

import (
	"database/sql"
	"fmt"
	"time"
)

func (db *DB) CreateGalleryImage(img *GalleryImage) error {
	_, err := db.Exec(`INSERT INTO gallery_images
		(id,filename,prompt,model,size,quality,tags,ai_tags,session_id,album_id,owner,
		 is_active,favorite,file_hash,taken_at,camera_make,camera_model,gps_lat,gps_lng,
		 width,height,file_size,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		img.ID, img.Filename, img.Prompt, img.Model, img.Size, img.Quality,
		img.Tags, img.AITags, img.SessionID, img.AlbumID, img.Owner,
		boolInt(img.IsActive), boolInt(img.Favorite), img.FileHash,
		img.TakenAt, img.CameraMake, img.CameraModel, img.GPSLat, img.GPSLng,
		img.Width, img.Height, img.FileSize, now(), now(),
	)
	return err
}

func (db *DB) GetGalleryImage(id string) (*GalleryImage, error) {
	row := db.QueryRow(`SELECT id,filename,prompt,model,size,quality,tags,ai_tags,session_id,album_id,
		owner,is_active,favorite,file_hash,taken_at,camera_make,camera_model,gps_lat,gps_lng,
		width,height,file_size,created_at,updated_at FROM gallery_images WHERE id=?`, id)
	return scanGalleryImage(row)
}

func (db *DB) GetGalleryImageByHash(hash, owner string) (*GalleryImage, error) {
	q := `SELECT id,filename,prompt,model,size,quality,tags,ai_tags,session_id,album_id,
		owner,is_active,favorite,file_hash,taken_at,camera_make,camera_model,gps_lat,gps_lng,
		width,height,file_size,created_at,updated_at FROM gallery_images WHERE file_hash=? AND is_active=1`
	args := []any{hash}
	if owner != "" {
		q += " AND owner=?"
		args = append(args, owner)
	}
	row := db.QueryRow(q, args...)
	return scanGalleryImage(row)
}

func (db *DB) ListGalleryImages(owner string, albumID string, limit, offset int) ([]*GalleryImage, error) {
	q := `SELECT id,filename,prompt,model,size,quality,tags,ai_tags,session_id,album_id,
		owner,is_active,favorite,file_hash,taken_at,camera_make,camera_model,gps_lat,gps_lng,
		width,height,file_size,created_at,updated_at FROM gallery_images WHERE is_active=1`
	args := []any{}
	if owner != "" {
		q += " AND owner=?"
		args = append(args, owner)
	}
	if albumID != "" {
		q += " AND album_id=?"
		args = append(args, albumID)
	}
	q += " ORDER BY created_at DESC"
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var images []*GalleryImage
	for rows.Next() {
		img, err := scanGalleryImage(rows)
		if err != nil {
			return nil, err
		}
		images = append(images, img)
	}
	return images, rows.Err()
}

func (db *DB) UpdateGalleryImage(img *GalleryImage) error {
	_, err := db.Exec(`UPDATE gallery_images SET
		prompt=?,tags=?,ai_tags=?,album_id=?,favorite=?,updated_at=? WHERE id=?`,
		img.Prompt, img.Tags, img.AITags, img.AlbumID, boolInt(img.Favorite), now(), img.ID,
	)
	return err
}

func (db *DB) DeleteGalleryImage(id string) error {
	_, err := db.Exec(`UPDATE gallery_images SET is_active=0,updated_at=? WHERE id=?`, now(), id)
	return err
}

func (db *DB) CreateGalleryAlbum(a *GalleryAlbum) error {
	_, err := db.Exec(`INSERT INTO gallery_albums (id,name,description,cover_id,owner,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?)`,
		a.ID, a.Name, a.Description, a.CoverID, a.Owner, now(), now(),
	)
	return err
}

func (db *DB) ListGalleryAlbums(owner string) ([]*GalleryAlbum, error) {
	q := `SELECT id,name,description,cover_id,owner,created_at,updated_at FROM gallery_albums`
	args := []any{}
	if owner != "" {
		q += " WHERE owner=?"
		args = append(args, owner)
	}
	q += " ORDER BY name"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var albums []*GalleryAlbum
	for rows.Next() {
		a := &GalleryAlbum{}
		var createdAt, updatedAt string
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.CoverID, &a.Owner, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		a.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		albums = append(albums, a)
	}
	return albums, rows.Err()
}

func (db *DB) UpdateGalleryAlbum(a *GalleryAlbum) error {
	_, err := db.Exec(`UPDATE gallery_albums SET name=?,description=?,cover_id=?,updated_at=? WHERE id=?`,
		a.Name, a.Description, a.CoverID, now(), a.ID)
	return err
}

func (db *DB) DeleteGalleryAlbum(id string) error {
	_, err := db.Exec(`DELETE FROM gallery_albums WHERE id=?`, id)
	return err
}

func (db *DB) CreateEditorDraft(d *EditorDraft) error {
	_, err := db.Exec(`INSERT INTO editor_drafts
		(id,owner,name,source_image_id,width,height,payload,thumbnail,is_active,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.Owner, d.Name, d.SourceImageID, d.Width, d.Height,
		d.Payload, d.Thumbnail, boolInt(d.IsActive), now(), now(),
	)
	return err
}

func (db *DB) GetEditorDraft(id string) (*EditorDraft, error) {
	row := db.QueryRow(`SELECT id,owner,name,source_image_id,width,height,payload,thumbnail,is_active,
		created_at,updated_at FROM editor_drafts WHERE id=?`, id)
	return scanEditorDraft(row)
}

func (db *DB) ListEditorDrafts(owner string) ([]*EditorDraft, error) {
	q := `SELECT id,owner,name,source_image_id,width,height,payload,thumbnail,is_active,
		created_at,updated_at FROM editor_drafts WHERE is_active=1`
	args := []any{}
	if owner != "" {
		q += " AND owner=?"
		args = append(args, owner)
	}
	q += " ORDER BY updated_at DESC"
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var drafts []*EditorDraft
	for rows.Next() {
		d, err := scanEditorDraft(rows)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, d)
	}
	return drafts, rows.Err()
}

func (db *DB) UpdateEditorDraft(d *EditorDraft) error {
	_, err := db.Exec(`UPDATE editor_drafts SET name=?,payload=?,thumbnail=?,updated_at=? WHERE id=?`,
		d.Name, d.Payload, d.Thumbnail, now(), d.ID)
	return err
}

func (db *DB) DeleteEditorDraft(id string) error {
	_, err := db.Exec(`UPDATE editor_drafts SET is_active=0,updated_at=? WHERE id=?`, now(), id)
	return err
}

func scanGalleryImage(row scanner) (*GalleryImage, error) {
	img := &GalleryImage{}
	var isActive, favorite int
	var createdAt, updatedAt string
	err := row.Scan(
		&img.ID, &img.Filename, &img.Prompt, &img.Model, &img.Size, &img.Quality,
		&img.Tags, &img.AITags, &img.SessionID, &img.AlbumID, &img.Owner,
		&isActive, &favorite, &img.FileHash, &img.TakenAt, &img.CameraMake,
		&img.CameraModel, &img.GPSLat, &img.GPSLng, &img.Width, &img.Height,
		&img.FileSize, &createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("image not found")
		}
		return nil, err
	}
	img.IsActive = isActive != 0
	img.Favorite = favorite != 0
	img.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	img.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return img, nil
}

func scanEditorDraft(row scanner) (*EditorDraft, error) {
	d := &EditorDraft{}
	var isActive int
	var createdAt, updatedAt string
	err := row.Scan(&d.ID, &d.Owner, &d.Name, &d.SourceImageID, &d.Width, &d.Height,
		&d.Payload, &d.Thumbnail, &isActive, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("draft not found")
		}
		return nil, err
	}
	d.IsActive = isActive != 0
	d.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	d.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return d, nil
}
