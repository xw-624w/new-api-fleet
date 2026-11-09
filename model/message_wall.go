package model

import "time"

// MessageWall 模型广场留言墙的一条留言。
// Username 是发布时的昵称快照，这样用户名后续变更或账号删除都不会影响历史留言展示。
type MessageWall struct {
	Id        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId    int    `json:"user_id" gorm:"index;not null"`
	Username  string `json:"username" gorm:"type:varchar(64);not null"`
	Content   string `json:"content" gorm:"type:text;not null"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;not null"`
}

func (MessageWall) TableName() string {
	return "message_walls"
}

// GetMessageWallMessages 按发布时间倒序分页读取留言，同时返回总数用于分页。
func GetMessageWallMessages(offset int, limit int) ([]MessageWall, int64, error) {
	var (
		messages []MessageWall
		total    int64
	)
	if err := DB.Model(&MessageWall{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := DB.Order("id DESC").Offset(offset).Limit(limit).Find(&messages).Error; err != nil {
		return nil, 0, err
	}
	return messages, total, nil
}

// CreateMessageWallMessage 写入一条留言。
func CreateMessageWallMessage(userId int, username string, content string) (*MessageWall, error) {
	message := &MessageWall{
		UserId:    userId,
		Username:  username,
		Content:   content,
		CreatedAt: time.Now().Unix(),
	}
	if err := DB.Create(message).Error; err != nil {
		return nil, err
	}
	return message, nil
}

// GetMessageWallMessageById 读取单条留言，用于删除前的归属校验。
func GetMessageWallMessageById(id int) (*MessageWall, error) {
	var message MessageWall
	if err := DB.Where("id = ?", id).First(&message).Error; err != nil {
		return nil, err
	}
	return &message, nil
}

// DeleteMessageWallMessage 按主键删除一条留言。权限判断在 controller 层完成。
func DeleteMessageWallMessage(id int) error {
	return DB.Where("id = ?", id).Delete(&MessageWall{}).Error
}
