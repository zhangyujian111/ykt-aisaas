package v1

import (
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/asr"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/metering"
	"ykt.dev/aisaas/internal/platform/quota"
	"ykt.dev/aisaas/internal/platform/redisx"
	"ykt.dev/aisaas/internal/platform/web"
	"ykt.dev/aisaas/internal/tts"
	"ykt.dev/aisaas/pkg/openaiclient"
)

// AudioHandler /v1/audio/speech + /v1/audio/transcriptions。
type AudioHandler struct {
	Tts   *tts.Service
	Asr   *asr.Service
	Quota *quota.Guard
	Meter *metering.Recorder
}

type speechBody struct {
	Model          string   `json:"model"`
	Input          string   `json:"input" binding:"required"`
	Voice          string   `json:"voice"`
	ResponseFormat string   `json:"response_format"`
	Speed          *float64 `json:"speed"`
	Emotion        string   `json:"x-emotion"`
}

// Speech POST /v1/audio/speech — 流式透传音频（chunked）+ 按字符计量。
func (h *AudioHandler) Speech(c *gin.Context) {
	ctx := c.Request.Context()
	var body speechBody
	if err := c.ShouldBindJSON(&body); err != nil {
		web.AbortOpenAI(c, errs.New(errs.InvalidJSON, err.Error()))
		return
	}
	chars := int64(utf8.RuneCountInString(body.Input))

	if !h.Quota.PrecheckDim(ctx, redisx.DimTTSChars, chars) {
		web.AbortOpenAI(c, errs.New(errs.QuotaExceeded))
		return
	}

	result, resolved, err := h.Tts.Synthesize(ctx, &openaiclient.SpeechRequest{
		Model: body.Model, Input: body.Input, Voice: body.Voice,
		ResponseFormat: body.ResponseFormat, Speed: body.Speed, Emotion: body.Emotion,
	})
	if err != nil {
		h.Meter.RecordWithCtx(ctx, metering.Record{
			BizType: metering.BizTTS, Dimension: metering.DimTTSChars,
			Amount: 0, ModelID: body.Model, Status: 0, RequestID: web.RequestID(c),
		})
		web.AbortOpenAI(c, err)
		return
	}
	defer result.Body.Close()

	// 预检通过 + 上游建连成功即计费（音频体透传中途断开按已合成计）
	h.Meter.RecordWithCtx(ctx, metering.Record{
		BizType: metering.BizTTS, Dimension: metering.DimTTSChars,
		Amount: chars, ModelID: resolved.ModelID, Status: 1, RequestID: web.RequestID(c),
	})

	c.Header("Content-Type", result.ContentType)
	c.Status(http.StatusOK)
	w := c.Writer
	fl, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, rerr := result.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
		if rerr != nil || ctx.Err() != nil {
			return
		}
	}
}

// Transcriptions POST /v1/audio/transcriptions — multipart 上传识别（VAD 切段后逐段调用）。
func (h *AudioHandler) Transcriptions(c *gin.Context) {
	ctx := c.Request.Context()
	fileHeader, err := c.FormFile("file")
	if err != nil {
		web.AbortOpenAI(c, errs.New(errs.InvalidParam, "缺少 file 字段"))
		return
	}
	modelID := c.PostForm("model")
	language := c.PostForm("language")
	if modelID == "" {
		web.AbortOpenAI(c, errs.New(errs.InvalidParam, "缺少 model 字段"))
		return
	}
	f, err := fileHeader.Open()
	if err != nil {
		web.AbortOpenAI(c, errs.Wrap(errs.Internal, err))
		return
	}
	defer f.Close()
	body := make([]byte, fileHeader.Size)
	if _, err := io.ReadFull(f, body); err != nil {
		web.AbortOpenAI(c, errs.Wrap(errs.Internal, err))
		return
	}

	if !h.Quota.PrecheckDim(ctx, redisx.DimASRSeconds, asr.EstimateSeconds(len(body), 0)) {
		web.AbortOpenAI(c, errs.New(errs.QuotaExceeded))
		return
	}

	result, resolved, err := h.Asr.Recognize(ctx, body, fileHeader.Filename, modelID, language)
	if err != nil {
		web.AbortOpenAI(c, err)
		return
	}
	seconds := asr.EstimateSeconds(len(body), result.Duration)
	h.Meter.RecordWithCtx(ctx, metering.Record{
		BizType: metering.BizASR, Dimension: metering.DimASRSeconds,
		Amount: seconds, ModelID: resolved.ModelID, Status: 1,
		RequestID: web.RequestID(c),
	})
	c.JSON(http.StatusOK, result)
}
