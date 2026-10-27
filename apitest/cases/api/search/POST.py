def run(c):
    a=c.values['student_a']['access_token']
    b=c.values['student_b']['access_token']
    for token in (a,b):
        for mode in ('keyword','vector','hybrid'):
            status,_,body=c.request('POST','/api/search',{'q':'课程','mode':mode,'limit':5},token)
            assert status==200,(status,body)
            assert body['mode']==mode and isinstance(body['hits'],list)
            user=c.values['student_a' if token==a else 'student_b']['user']
            for hit in body['hits']:
                material_id=hit['material_id']
                assert c.db_scalar(f'SELECT class_id FROM materials WHERE id={material_id};')==str(user['classId'])
    status,_,body=c.request('POST','/api/search',{'q':'课程','mode':'invalid'},a)
    assert status==400 and 'error' in body
    return '关键字/向量/混合检索均返回本班 MySQL 材料；非法模式 400'
