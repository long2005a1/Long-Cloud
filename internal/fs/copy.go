package fs

import (
	"context"
	"fmt"
	"net/http"
	stdpath "path"

	"github.com/alist-org/alist/v3/internal/conf"
	"github.com/alist-org/alist/v3/internal/driver"
	"github.com/alist-org/alist/v3/internal/model"
	"github.com/alist-org/alist/v3/internal/op"
	"github.com/alist-org/alist/v3/internal/stream"
	"github.com/alist-org/alist/v3/internal/task"
	"github.com/alist-org/alist/v3/pkg/utils"
	"github.com/pkg/errors"
	"github.com/xhofe/tache"
)

type CopyTask struct {
	task.TaskWithCreator
	Status       string        `json:"-"` //don't save status to save space
	SrcObjPath   string        `json:"src_path"`
	DstDirPath   string        `json:"dst_path"`
	srcStorage   driver.Driver `json:"-"`
	dstStorage   driver.Driver `json:"-"`
	SrcStorageMp string        `json:"src_storage_mp"`
	DstStorageMp string        `json:"dst_storage_mp"`
}

func (t *CopyTask) GetName() string {
	return fmt.Sprintf("复制 [%s](%s) 到 [%s](%s)", t.SrcStorageMp, t.SrcObjPath, t.DstStorageMp, t.DstDirPath)
}

func (t *CopyTask) GetStatus() string {
	return t.Status
}

func (t *CopyTask) Run() error {
	var err error
	if t.srcStorage == nil {
		t.srcStorage, err = op.GetStorageByMountPath(t.SrcStorageMp)
	}
	if t.dstStorage == nil {
		t.dstStorage, err = op.GetStorageByMountPath(t.DstStorageMp)
	}
	if err != nil {
		return errors.WithMessage(err, "无法获取存储")
	}
	return copyBetween2Storages(t, t.srcStorage, t.dstStorage, t.SrcObjPath, t.DstDirPath)
}

var CopyTaskManager *tache.Manager[*CopyTask]

// Copy if in the same storage, call move method
// if not, add copy task
func _copy(ctx context.Context, srcObjPath, dstDirPath string, lazyCache ...bool) (task.TaskInfoWithCreator, error) {
	srcStorage, srcObjActualPath, err := op.GetStorageAndActualPath(srcObjPath)
	if err != nil {
		return nil, errors.WithMessage(err, "获取源存储失败")
	}
	dstStorage, dstDirActualPath, err := op.GetStorageAndActualPath(dstDirPath)
	if err != nil {
		return nil, errors.WithMessage(err, "获取目标存储失败")
	}
	// copy if in the same storage, just call driver.Copy
	if srcStorage.GetStorage() == dstStorage.GetStorage() {
		return nil, op.Copy(ctx, srcStorage, srcObjActualPath, dstDirActualPath, lazyCache...)
	}
	if ctx.Value(conf.NoTaskKey) != nil {
		srcObj, err := op.Get(ctx, srcStorage, srcObjActualPath)
		if err != nil {
			return nil, errors.WithMessagef(err, "源文件 [%s] 获取失败", srcObjPath)
		}
		if !srcObj.IsDir() {
			// copy file directly
			link, _, err := op.Link(ctx, srcStorage, srcObjActualPath, model.LinkArgs{
				Header: http.Header{},
			})
			if err != nil {
				return nil, errors.WithMessagef(err, "链接 [%s] 获取失败", srcObjPath)
			}
			fs := stream.FileStream{
				Obj: srcObj,
				Ctx: ctx,
			}
			// any link provided is seekable
			ss, err := stream.NewSeekableStream(fs, link)
			if err != nil {
				return nil, errors.WithMessagef(err, "流媒体 [%s] 获取失败", srcObjPath)
			}
			return nil, op.Put(ctx, dstStorage, dstDirActualPath, ss, nil, false)
		}
	}
	// not in the same storage
	taskCreator, _ := ctx.Value("user").(*model.User) // taskCreator is nil when convert failed
	t := &CopyTask{
		TaskWithCreator: task.TaskWithCreator{
			Creator: taskCreator,
		},
		srcStorage:   srcStorage,
		dstStorage:   dstStorage,
		SrcObjPath:   srcObjActualPath,
		DstDirPath:   dstDirActualPath,
		SrcStorageMp: srcStorage.GetStorage().MountPath,
		DstStorageMp: dstStorage.GetStorage().MountPath,
	}
	CopyTaskManager.Add(t)
	return t, nil
}

func copyBetween2Storages(t *CopyTask, srcStorage, dstStorage driver.Driver, srcObjPath, dstDirPath string) error {
	t.Status = "获取源对象"
	srcObj, err := op.Get(t.Ctx(), srcStorage, srcObjPath)
	if err != nil {
		return errors.WithMessagef(err, "源文件 [%s] 获取失败", srcObjPath)
	}
	if srcObj.IsDir() {
		t.Status = "src object is dir, listing objs"
		objs, err := op.List(t.Ctx(), srcStorage, srcObjPath, model.ListArgs{})
		if err != nil {
			return errors.WithMessagef(err, "源对象 [%s] 获取失败", srcObjPath)
		}
		for _, obj := range objs {
			if utils.IsCanceled(t.Ctx()) {
				return nil
			}
			srcObjPath := stdpath.Join(srcObjPath, obj.GetName())
			dstObjPath := stdpath.Join(dstDirPath, srcObj.GetName())
			CopyTaskManager.Add(&CopyTask{
				TaskWithCreator: task.TaskWithCreator{
					Creator: t.Creator,
				},
				srcStorage:   srcStorage,
				dstStorage:   dstStorage,
				SrcObjPath:   srcObjPath,
				DstDirPath:   dstObjPath,
				SrcStorageMp: srcStorage.GetStorage().MountPath,
				DstStorageMp: dstStorage.GetStorage().MountPath,
			})
		}
		t.Status = "源对象为文件夹, 已添加了所有对象的复制任务"
		return nil
	}
	return copyFileBetween2Storages(t, srcStorage, dstStorage, srcObjPath, dstDirPath)
}

func copyFileBetween2Storages(tsk *CopyTask, srcStorage, dstStorage driver.Driver, srcFilePath, dstDirPath string) error {
	srcFile, err := op.Get(tsk.Ctx(), srcStorage, srcFilePath)
	if err != nil {
		return errors.WithMessagef(err, "源文件 [%s] 获取失败", srcFilePath)
	}
	link, _, err := op.Link(tsk.Ctx(), srcStorage, srcFilePath, model.LinkArgs{
		Header: http.Header{},
	})
	if err != nil {
		return errors.WithMessagef(err, "链接 [%s] 获取失败", srcFilePath)
	}
	fs := stream.FileStream{
		Obj: srcFile,
		Ctx: tsk.Ctx(),
	}
	// any link provided is seekable
	ss, err := stream.NewSeekableStream(fs, link)
	if err != nil {
		return errors.WithMessagef(err, "流媒体 [%s] 获取失败", srcFilePath)
	}
	return op.Put(tsk.Ctx(), dstStorage, dstDirPath, ss, tsk.SetProgress, true)
}
