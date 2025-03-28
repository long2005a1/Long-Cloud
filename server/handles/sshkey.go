package handles

import (
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/server/common"
	"github.com/gin-gonic/gin"
	"strconv"
	"strings"
)

type SSHKeyAddReq struct {
	Title string `json:"title" binding:"required"`
	Key   string `json:"key" binding:"required"`
}

func AddMyPublicKey(c *gin.Context) {
	userObj, ok := c.Value("user").(*model.User)
	if !ok || userObj.IsGuest() {
		common.ErrorStrResp(c, "当前用户无效", 401)
		return
	}
	var req SSHKeyAddReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorStrResp(c, "请求无效", 400)
		return
	}
	if req.Title == "" {
		common.ErrorStrResp(c, "请求无效", 400)
		return
	}
	key := &model.SSHPublicKey{
		Title:  req.Title,
		KeyStr: strings.TrimSpace(req.Key),
		UserId: userObj.ID,
	}
	err, parsed := op.CreateSSHPublicKey(key)
	if !parsed {
		common.ErrorStrResp(c, "提供的密钥无效", 400)
		return
	} else if err != nil {
		common.ErrorStrResp(c, "创建失败", 500, true)
		return
	}
	common.SuccessResp(c)
}

func ListMyPublicKey(c *gin.Context) {
	userObj, ok := c.Value("user").(*model.User)
	if !ok || userObj.IsGuest() {
		common.ErrorStrResp(c, "当前用户无效", 401)
		return
	}
	list(c, userObj)
}

func DeleteMyPublicKey(c *gin.Context) {
	userObj, ok := c.Value("user").(*model.User)
	if !ok || userObj.IsGuest() {
		common.ErrorStrResp(c, "当前用户无效", 401)
		return
	}
	keyId, err := strconv.Atoi(c.Query("id"))
	if err != nil {
		common.ErrorStrResp(c, "ID格式无效", 400)
		return
	}
	key, err := op.GetSSHPublicKeyByIdAndUserId(uint(keyId), userObj.ID)
	if err != nil {
		common.ErrorStrResp(c, "获取公钥失败", 404)
		return
	}
	err = op.DeleteSSHPublicKeyById(key.ID)
	if err != nil {
		common.ErrorStrResp(c, "删除失败", 500, true)
		return
	}
	common.SuccessResp(c)
}

func ListPublicKeys(c *gin.Context) {
	userId, err := strconv.Atoi(c.Query("uid"))
	if err != nil {
		common.ErrorStrResp(c, "用户ID格式无效", 400)
		return
	}
	userObj, err := op.GetUserById(uint(userId))
	if err != nil {
		common.ErrorStrResp(c, "当前用户无效", 404)
		return
	}
	list(c, userObj)
}

func DeletePublicKey(c *gin.Context) {
	keyId, err := strconv.Atoi(c.Query("id"))
	if err != nil {
		common.ErrorStrResp(c, "ID格式无效", 400)
		return
	}
	err = op.DeleteSSHPublicKeyById(uint(keyId))
	if err != nil {
		common.ErrorStrResp(c, "删除失败", 500, true)
		return
	}
	common.SuccessResp(c)
}

func list(c *gin.Context, userObj *model.User) {
	var req model.PageReq
	if err := c.ShouldBind(&req); err != nil {
		common.ErrorStrResp(c, "清单获取失败", 400)
		return
	}
	req.Validate()
	keys, total, err := op.GetSSHPublicKeyByUserId(userObj.ID, req.Page, req.PerPage)
	if err != nil {
		common.ErrorStrResp(c, "验证失败", 500, true)
		return
	}
	common.SuccessResp(c, common.PageResp{
		Content: keys,
		Total:   total,
	})
}