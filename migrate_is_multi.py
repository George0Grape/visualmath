import sqlite3, json

db_path = '/home/nikitin/website_visualmath/visualmath/visualmath.db'
conn = sqlite3.connect(db_path)
c = conn.cursor()

c.execute("SELECT id, title, content FROM modules WHERE module_type='test'")
rows = c.fetchall()

for mod_id, title, content in rows:
    try:
        data = json.loads(content)
    except Exception as e:
        print(f'Module {mod_id} ({title}): JSON parse error: {e}')
        continue

    if isinstance(data, list):
        questions = data
        for q in questions:
            q['is_multi'] = True
        new_content = json.dumps(data, ensure_ascii=False)
    elif isinstance(data, dict) and 'questions' in data:
        for q in data['questions']:
            q['is_multi'] = True
        new_content = json.dumps(data, ensure_ascii=False)
    else:
        print(f'Module {mod_id} ({title}): unknown format, skipping')
        continue

    c.execute("UPDATE modules SET content=? WHERE id=?", (new_content, mod_id))
    print(f'Module {mod_id} ({title}): updated')

conn.commit()
conn.close()
print('Done.')
