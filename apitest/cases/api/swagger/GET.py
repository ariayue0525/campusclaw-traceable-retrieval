import urllib.request

def run(c):
    with urllib.request.urlopen(c.base+'/swagger',timeout=20) as response:
        page=response.read().decode()
        assert response.status==200 and 'SwaggerUIBundle' in page
    status,_,body=c.request('GET','/openapi.json')
    assert status==200 and body['openapi']=='3.0.3'
    assert '/api/conversations/{id}/messages/stream' in body['paths']
    assert body['components']['securitySchemes']['bearerAuth']['scheme']=='bearer'
    return f"Swagger UI 与 OpenAPI JSON 可访问，定义 {len(body['paths'])} 条路径，Bearer 方案存在"
