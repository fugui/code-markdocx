package imgprep

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"

	"code-markdocx/pkg/docxparser"
)

type PrepOptions struct {
	MinImagePx          int
	MaxImageDim         int
	DedupeLogoThreshold int
	SofficePath         string
	OutputDir           string
}

type PreparedImage struct {
	Asset        *docxparser.ImageAsset
	PreparedPath string
	PreparedCT   string
	PreparedData []byte
}

func PrepareImages(assets []*docxparser.ImageAsset, opts PrepOptions) ([]*PreparedImage, error) {
	if opts.MinImagePx <= 0 {
		opts.MinImagePx = 48
	}
	if opts.MaxImageDim <= 0 {
		opts.MaxImageDim = 2048
	}
	if opts.DedupeLogoThreshold <= 0 {
		opts.DedupeLogoThreshold = 3
	}

	// Count duplicate SHA256 hashes (repeated header/footer logos)
	hashCounts := make(map[string]int, len(assets))
	for _, a := range assets {
		hashCounts[a.SHA256]++
	}

	soffice := DetectSoffice(opts.SofficePath)
	results := make([]*PreparedImage, 0, len(assets))

	for _, a := range assets {
		item := &PreparedImage{Asset: a}

		if hashCounts[a.SHA256] >= opts.DedupeLogoThreshold {
			a.PreSkipped = true
			a.SkipReason = fmt.Sprintf("重复出现 %d 次的图标/Logo（自动跳过）", hashCounts[a.SHA256])
			results = append(results, item)
			continue
		}

		if IsVectorFormat(a.Ext, a.RawBytes) {
			if soffice == "" {
				a.UnsupportedVector = true
				a.VectorWarning = fmt.Sprintf("检测到 %s 矢量图（如 Visio/Word 绘图），当前运行环境未安装 LibreOffice，无法自动转 PNG；建议在 Word 中将其替换为 PNG 截图或手动补充 Mermaid。", a.Ext)
				results = append(results, item)
				continue
			}
			pngBytes, convErr := ConvertVectorToPNG(soffice, a.Ext, a.RawBytes)
			if convErr != nil {
				a.UnsupportedVector = true
				a.VectorWarning = fmt.Sprintf("检测到 %s 矢量图，调用 LibreOffice 转换失败（%v）；建议替换为 PNG 截图。", a.Ext, convErr)
				results = append(results, item)
				continue
			}
			item.PreparedData = pngBytes
			item.PreparedCT = "image/png"
		} else {
			img, format, decErr := image.Decode(bytes.NewReader(a.RawBytes))
			if decErr != nil {
				a.PreSkipped = true
				a.SkipReason = fmt.Sprintf("无法解码图片格式 (%s)", a.Ext)
				results = append(results, item)
				continue
			}
			bounds := img.Bounds()
			w, h := bounds.Dx(), bounds.Dy()
			a.Width = w
			a.Height = h

			if w < opts.MinImagePx && h < opts.MinImagePx {
				a.PreSkipped = true
				a.SkipReason = fmt.Sprintf("尺寸过小 (%dx%d < %dpx)，判定为小图标", w, h, opts.MinImagePx)
				results = append(results, item)
				continue
			}

			preparedBytes, preparedCT, newW, newH, err := resizeAndEncode(img, format, a.RawBytes, opts.MaxImageDim)
			if err != nil {
				item.PreparedData = a.RawBytes
				item.PreparedCT = a.ContentType
			} else {
				item.PreparedData = preparedBytes
				item.PreparedCT = preparedCT
				a.Width = newW
				a.Height = newH
			}
		}

		// Decode dimensions if converted from vector
		if a.Width == 0 && len(item.PreparedData) > 0 {
			if cfg, _, err := image.DecodeConfig(bytes.NewReader(item.PreparedData)); err == nil {
				a.Width = cfg.Width
				a.Height = cfg.Height
			}
		}

		if opts.OutputDir != "" && len(item.PreparedData) > 0 {
			if err := os.MkdirAll(opts.OutputDir, 0750); err != nil {
				return nil, fmt.Errorf("create image output dir: %w", err)
			}
			ext := ".png"
			if item.PreparedCT == "image/jpeg" {
				ext = ".jpg"
			}
			outPath := filepath.Join(opts.OutputDir, a.ID+ext)
			if err := os.WriteFile(outPath, item.PreparedData, 0640); err != nil {
				return nil, fmt.Errorf("write prepared image %s: %w", outPath, err)
			}
			item.PreparedPath = outPath
		}

		results = append(results, item)
	}
	return results, nil
}

func resizeAndEncode(src image.Image, format string, origBytes []byte, maxDim int) ([]byte, string, int, int, error) {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxDim && h <= maxDim && (format == "png" || format == "jpeg") {
		ct := "image/png"
		if format == "jpeg" {
			ct = "image/jpeg"
		}
		return origBytes, ct, w, h, nil
	}

	newW, newH := w, h
	if w > maxDim || h > maxDim {
		if w >= h {
			newW = maxDim
			newH = max(1, h*maxDim/w)
		} else {
			newH = maxDim
			newW = max(1, w*maxDim/h)
		}
	}

	dst := downscaleBilinear(src, newW, newH)
	var out bytes.Buffer
	if format == "jpeg" {
		if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 88}); err != nil {
			return nil, "", w, h, err
		}
		return out.Bytes(), "image/jpeg", newW, newH, nil
	}
	if err := png.Encode(&out, dst); err != nil {
		return nil, "", w, h, err
	}
	return out.Bytes(), "image/png", newW, newH, nil
}

func downscaleBilinear(src image.Image, dstW, dstH int) *image.RGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	if sw == 0 || sh == 0 {
		return dst
	}
	for y := 0; y < dstH; y++ {
		sy := float64(y) * float64(sh) / float64(dstH)
		y0 := int(sy)
		y1 := min(y0+1, sh-1)
		fy := sy - float64(y0)
		for x := 0; x < dstW; x++ {
			sx := float64(x) * float64(sw) / float64(dstW)
			x0 := int(sx)
			x1 := min(x0+1, sw-1)
			fx := sx - float64(x0)

			c00 := color.NRGBAModel.Convert(src.At(sb.Min.X+x0, sb.Min.Y+y0)).(color.NRGBA)
			c10 := color.NRGBAModel.Convert(src.At(sb.Min.X+x1, sb.Min.Y+y0)).(color.NRGBA)
			c01 := color.NRGBAModel.Convert(src.At(sb.Min.X+x0, sb.Min.Y+y1)).(color.NRGBA)
			c11 := color.NRGBAModel.Convert(src.At(sb.Min.X+x1, sb.Min.Y+y1)).(color.NRGBA)

			lerp := func(a, b, c, d uint8) uint8 {
				top := float64(a)*(1-fx) + float64(b)*fx
				bot := float64(c)*(1-fx) + float64(d)*fx
				return uint8(top*(1-fy) + bot*fy + 0.5)
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: lerp(c00.R, c10.R, c01.R, c11.R),
				G: lerp(c00.G, c10.G, c01.G, c11.G),
				B: lerp(c00.B, c10.B, c01.B, c11.B),
				A: lerp(c00.A, c10.A, c01.A, c11.A),
			})
		}
	}
	return dst
}
