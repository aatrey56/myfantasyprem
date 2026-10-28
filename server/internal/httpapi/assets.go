package httpapi

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"github.com/aatrey56/myfantasyprem/server/internal/assets"
)

// imageCacheControl lets browsers and proxies keep an image for a week.
const imageCacheControl = "public, max-age=604800"

// GetAppConfig reports the client settings: whether branded images are on.
func (s *Server) GetAppConfig(context.Context, GetAppConfigRequestObject) (GetAppConfigResponseObject, error) {
	now := s.now().UTC()
	return GetAppConfig200JSONResponse{
		Data: AppConfig{BrandedAssets: s.assets.On(), WebPushPublicKey: nullString(s.webPushKey)},
		Meta: Meta{DataAsOf: now, ServerTime: now},
	}, nil
}

// imageNotFound is the body of every missing image: switched off, unknown
// id, no such image upstream or upstream failing look the same.
func imageNotFound(ctx context.Context) Problem {
	return newProblem(404, "not-found", "Not found", "No such image.", RequestIDFrom(ctx))
}

// imageResponse turns an assets result into the 200, 304 or 404 response.
func imageResponse[T any](ctx context.Context, img *assets.Image, err error, ifNoneMatch *string,
	ok func(ImageImageResponse) T, notModified func(NotModifiedResponse) T, notFound func(Problem) T,
) (T, error) {
	if errors.Is(err, assets.ErrNotFound) {
		return notFound(imageNotFound(ctx)), nil
	}
	if err != nil {
		var zero T
		return zero, err
	}
	if ifNoneMatch != nil && etagMatches(*ifNoneMatch, img.ETag) {
		return notModified(NotModifiedResponse{Headers: NotModifiedResponseHeaders{CacheControl: imageCacheControl, ETag: img.ETag}}), nil
	}
	return ok(ImageImageResponse{
		Body:          bytes.NewReader(img.Body),
		ContentType:   img.ContentType,
		ContentLength: int64(len(img.Body)),
		Headers:       ImageResponseHeaders{CacheControl: imageCacheControl, ETag: img.ETag},
	}), nil
}

// etagMatches implements If-None-Match's weak comparison.
func etagMatches(header, etag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimPrefix(strings.TrimSpace(t), "W/")
		if t == "*" || t == etag {
			return true
		}
	}
	return false
}

func (s *Server) GetPlayerPhoto(ctx context.Context, req GetPlayerPhotoRequestObject) (GetPlayerPhotoResponseObject, error) {
	if d := badID("playerId", "plr", req.PlayerId); d != "" {
		return GetPlayerPhoto400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	img, err := s.assets.PlayerPhoto(ctx, req.PlayerId)
	return imageResponse(ctx, img, err, req.Params.IfNoneMatch,
		func(r ImageImageResponse) GetPlayerPhotoResponseObject { return GetPlayerPhoto200ImageResponse{r} },
		func(r NotModifiedResponse) GetPlayerPhotoResponseObject { return GetPlayerPhoto304Response(r) },
		func(p Problem) GetPlayerPhotoResponseObject {
			return GetPlayerPhoto404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse(p)}
		})
}

// badClubCode returns a 400 detail unless code looks like ClubRef.code.
func badClubCode(code string) string {
	if n := len(code); n < 2 || n > 4 {
		return "clubCode must be a club's 2 to 4 letter code."
	}
	for _, c := range code {
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return "clubCode must be a club's 2 to 4 letter code."
		}
	}
	return ""
}

func (s *Server) GetClubCrest(ctx context.Context, req GetClubCrestRequestObject) (GetClubCrestResponseObject, error) {
	if d := badClubCode(req.ClubCode); d != "" {
		return GetClubCrest400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	img, err := s.assets.ClubCrest(ctx, req.ClubCode)
	return imageResponse(ctx, img, err, req.Params.IfNoneMatch,
		func(r ImageImageResponse) GetClubCrestResponseObject { return GetClubCrest200ImageResponse{r} },
		func(r NotModifiedResponse) GetClubCrestResponseObject { return GetClubCrest304Response(r) },
		func(p Problem) GetClubCrestResponseObject {
			return GetClubCrest404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse(p)}
		})
}

func (s *Server) GetClubShirt(ctx context.Context, req GetClubShirtRequestObject) (GetClubShirtResponseObject, error) {
	if d := badClubCode(req.ClubCode); d != "" {
		return GetClubShirt400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	gk := req.Params.Gk != nil && *req.Params.Gk
	img, err := s.assets.ClubShirt(ctx, req.ClubCode, gk)
	return imageResponse(ctx, img, err, req.Params.IfNoneMatch,
		func(r ImageImageResponse) GetClubShirtResponseObject { return GetClubShirt200ImageResponse{r} },
		func(r NotModifiedResponse) GetClubShirtResponseObject { return GetClubShirt304Response(r) },
		func(p Problem) GetClubShirtResponseObject {
			return GetClubShirt404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse(p)}
		})
}
