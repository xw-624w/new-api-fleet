package controller

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const (
	// messageWallMaxContentRunes 按字符（非字节）计数，避免中文留言被误判超长。
	messageWallMaxContentRunes = 500
	messageWallDefaultPageSize = 20
	messageWallMaxPageSize     = 50
)

// GetMessageWall 模型广场留言墙列表。与模型广场共用 pricing 导航模块开关，
// 未登录也可查看；是否已登录由 can_post 告知前端。
func GetMessageWall(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", strconv.Itoa(messageWallDefaultPageSize)))
	if pageSize < 1 {
		pageSize = messageWallDefaultPageSize
	}
	if pageSize > messageWallMaxPageSize {
		pageSize = messageWallMaxPageSize
	}

	messages, total, err := model.GetMessageWallMessages((page-1)*pageSize, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"messages":  messages,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"can_post":  c.GetInt("id") > 0,
	})
}

// AddMessageWall 发布留言，仅登录用户可用。
func AddMessageWall(c *gin.Context) {
	userId := c.GetInt("id")
	if userId <= 0 {
		common.ApiErrorMsg(c, "请先登录后再留言")
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "请求参数错误")
		return
	}

	content := strings.TrimSpace(req.Content)
	if content == "" {
		common.ApiErrorMsg(c, "留言内容不能为空")
		return
	}
	if utf8.RuneCountInString(content) > messageWallMaxContentRunes {
		common.ApiErrorMsg(c, fmt.Sprintf("留言内容不能超过 %d 个字符", messageWallMaxContentRunes))
		return
	}

	message, err := model.CreateMessageWallMessage(userId, c.GetString("username"), content)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, message)
}

// DeleteMessageWall 删除留言：作者本人可删自己的，管理员可删任意一条。
func DeleteMessageWall(c *gin.Context) {
	userId := c.GetInt("id")
	if userId <= 0 {
		common.ApiErrorMsg(c, "请先登录")
		return
	}

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "无效的留言 ID")
		return
	}

	message, err := model.GetMessageWallMessageById(id)
	if err != nil {
		common.ApiErrorMsg(c, "留言不存在")
		return
	}

	if message.UserId != userId && c.GetInt("role") < common.RoleAdminUser {
		common.ApiErrorMsg(c, "无权删除该留言")
		return
	}

	if err := model.DeleteMessageWallMessage(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
