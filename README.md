# Docker Cache Server

LRU TTL 기반 자동 삭제를 지원하는 Docker Registry Server 구현체입니다.

## 주요 기능

- ✅ Docker Registry v2 API 완벽 지원 (distribution/distribution 기반)
- ✅ Layer 및 Tag에 대한 LRU (Least Recently Used) TTL 자동 삭제
- ✅ Layer read/write 시 LRU 시간 자동 갱신
- ✅ 주기적 cleanup (기본 1시간마다)
- ✅ Basic 인증 지원 (htpasswd)
- ✅ 유연한 설정 (YAML, 환경 변수, 커맨드 라인 플래그)
- ✅ 라이브러리로 사용 가능한 구조

## 설치

```bash
go get github.com/jc-lab/docker-cache-server
```

## 빠른 시작

### 1. 설정 파일 생성

[`config.yaml`](config.yaml:1) 파일을 생성합니다:

```yaml
server:
  address: "0.0.0.0"
  port: 5000

storage:
  directory: "/var/cache/docker-cache-server"

auth:
  enabled: true
  users:
    - username: "admin"
      password: "admin123"
    - username: "user1"
      password: "password1"

cache:
  ttl: "30d"              # 30일 후 자동 삭제
  cleanup_interval: "1h"  # 1시간마다 cleanup 실행
```

### 2. 서버 실행

```bash
# 설정 파일 사용
./docker-cache-server --config config.yaml

# 커맨드 라인 플래그 사용
./docker-cache-server --server.port=5000 --cache.ttl=720h

# 환경 변수 사용
export DCS_SERVER_PORT=5000
export DCS_CACHE_TTL=720h
./docker-cache-server
```

### 3. Docker 클라이언트 설정

```bash
# 로그인
docker login localhost:5000 -u admin -p admin123

# 이미지 push
docker tag myimage:latest localhost:5000/myimage:latest
docker push localhost:5000/myimage:latest

# 이미지 pull
docker pull localhost:5000/myimage:latest
```

## 설정 옵션

### Server

- [`server.address`](config.example.yaml:4): 바인드 주소 (기본값: "0.0.0.0")
- [`server.port`](config.example.yaml:5): 포트 번호 (기본값: 5000)

### Storage

- [`storage.directory`](config.example.yaml:8): 저장소 디렉토리 경로 (기본값: "/var/cache/docker-cache-server")

### Auth

- [`auth.enabled`](config.example.yaml:11): 인증 활성화 여부 (기본값: true)
- [`auth.users`](config.example.yaml:12): 사용자 목록 (username, password)

### Cache

- [`cache.ttl`](config.example.yaml:20): 캐시 TTL (예: "30d", "720h", "43200m")
- [`cache.cleanup_interval`](config.example.yaml:22): Cleanup 주기 (예: "1h", "60m")

## 라이브러리로 사용하기

다른 Go 프로젝트에서 라이브러리로 사용할 수 있습니다:

```go
package main

import (
    "github.com/jc-lab/docker-cache-server/pkg/config"
    "github.com/jc-lab/docker-cache-server/pkg/server"
    "github.com/sirupsen/logrus"
)

func main() {
    // 설정 생성
    cfg := config.DefaultConfig()
    cfg.Server.Port = 5000
    cfg.Cache.TTL = 30 * 24 * time.Hour // 30 days
    
    // 커스텀 로거
    logger := logrus.New()
    logger.SetLevel(logrus.DebugLevel)
    
    // 서버 생성 및 시작
    srv, err := server.New(&server.Options{
        Config: cfg,
        Logger: logger,
        // 커스텀 인증 함수 (선택사항)
        AuthValidator: func(username, password string) bool {
            // 여기에 커스텀 인증 로직 구현
            return username == "custom" && password == "pass"
        },
        // Blob 액세스 콜백 (선택사항)
        OnBlobAccess: func(digest string, size int64) {
            logger.Infof("Blob accessed: %s (size: %d)", digest, size)
        },
    })
    if err != nil {
        logger.Fatal(err)
    }
    
    if err := srv.Start(); err != nil {
        logger.Fatal(err)
    }
}
```

## 아키텍처

```
┌─────────────────────────────────────────┐
│         Docker Client                    │
└──────────────┬──────────────────────────┘
               │ Docker Registry v2 API
┌──────────────▼──────────────────────────┐
│      Authentication Middleware           │
│      (Basic Auth / htpasswd)            │
└──────────────┬──────────────────────────┘
               │
┌──────────────▼──────────────────────────┐
│   Distribution Handlers (gorilla/mux)   │
│   - /v2/                                 │
│   - /v2/{name}/manifests/{reference}    │
│   - /v2/{name}/blobs/{digest}           │
│   - /v2/{name}/blobs/uploads/           │
└──────────────┬──────────────────────────┘
               │
┌──────────────▼──────────────────────────┐
│        LRU Tracking Driver               │
│   (wraps storage driver)                │
│   - Records access times                │
│   - Updates on read/write               │
└──────────────┬──────────────────────────┘
               │
┌──────────────▼──────────────────────────┐
│     Filesystem Storage Driver            │
│   (github.com/distribution/distribution)│
└──────────────┬──────────────────────────┘
               │
┌──────────────▼──────────────────────────┐
│        Disk Storage                      │
│   /var/cache/docker-cache-server/       │
│   ├── docker/                           │
│   │   └── registry/v2/                  │
│   │       ├── blobs/                    │
│   │       └── repositories/             │
│   └── .metadata/                        │
│       └── (LRU tracking data)           │
└─────────────────────────────────────────┘

     ┌─────────────────────┐
     │  LRU Cleanup Worker │
     │  (runs periodically)│
     └─────────────────────┘
```

## LRU TTL 작동 방식

1. **Access Tracking**: blob을 읽거나 쓸 때마다 last access 시간이 업데이트됩니다
2. **TTL Check**: cleanup worker가 주기적으로 실행되어 TTL이 지난 blob을 확인합니다
3. **Automatic Deletion**: TTL이 지난 blob은 자동으로 삭제됩니다
4. **Metadata Persistence**: LRU 메타데이터는 디스크에 저장되어 서버 재시작 시에도 유지됩니다

## 설정 우선순위

1. 커맨드 라인 플래그 (최우선)
2. 환경 변수 (`DCS_` prefix)
3. 설정 파일 (YAML)
4. 기본값

예시:
```bash
# 환경 변수로 포트 설정
export DCS_SERVER_PORT=8080

# 플래그가 환경 변수보다 우선
./docker-cache-server --server.port=9000  # 9000 사용됨
```

## 빌드

```bash
# 바이너리 빌드
go build -o docker-cache-server ./cmd/server

# Docker 이미지 빌드
docker build -t docker-cache-server:latest .
```

## 개발

```bash
# 의존성 다운로드
go mod download

# 테스트 실행
go test ./...

# 개발 모드로 실행
go run cmd/server/main.go --config config.example.yaml
```

## 라이선스

Apache License 2.0

## 기여

이 프로젝트는 [github.com/distribution/distribution](https://github.com/distribution/distribution)을 기반으로 합니다.