"""Local visual comparisons only. These images never enter the site build."""
from pathlib import Path
from PIL import Image, ImageDraw

root = Path('/home/cristhian/projects/ui-conceptos/landing')
out = root / 'implementacion/final'
review = out / 'review'
review.mkdir(exist_ok=True)
references = {
    'hero': root / 'landing-1-one-line.png',
    'machine': root / 'landing-4-blueprint.png',
    'your-day': root / 'resto/seccion-1-your-day.png',
    'memory': root / 'resto/seccion-2-memory.png',
    'data': root / 'resto/seccion-3-data.png',
    'code': root / 'resto/seccion-4-code.png',
    'work': root / 'resto/seccion-5-work.png',
    'measured': root / 'resto/seccion-6-benchmark.png',
    'extensions': root / 'resto/seccion-7-extensions.png',
    'install': None,
}
for section, reference in references.items():
    sheet = Image.new('RGB', (1920, 1120), '#e6eaf0')
    draw = ImageDraw.Draw(sheet)
    tiles = [(reference, 'Approved concept')]
    tiles += [(out / f'{section}-{width}-light.png', f'{width} / light') for width in [1280,1536,1920]]
    tiles += [(out / f'{section}-{width}-dark.png', f'{width} / dark') for width in [1280,1536,1920]]
    tiles += [(out / f'{section}-390-light.png', '390 / light'), (out / f'{section}-390-dark.png', '390 / dark')]
    for i, (path, label) in enumerate(tiles):
        x = (i % 4) * 480
        y = (i // 4) * 370
        draw.text((x+10, y+8), label, fill='#0b1220')
        if path and path.exists():
            picture = Image.open(path).convert('RGB')
            picture.thumbnail((476, 340))
            sheet.paste(picture, (x+(480-picture.width)//2, y+28))
    sheet.save(review / f'{section}-comparison.jpg', quality=92)
for theme in ['light', 'dark']:
    sheet = Image.new('RGB', (1920, 1440), '#e6eaf0')
    draw = ImageDraw.Draw(sheet)
    for i, section in enumerate(references):
        path = out / f'{section}-1536-{theme}-mid.png'
        if not path.exists():
            continue
        x, y = (i % 3) * 640, (i // 3) * 360
        draw.text((x+8, y+4), section, fill='#0b1220')
        picture = Image.open(path).convert('RGB')
        picture.thumbnail((638, 330))
        sheet.paste(picture, (x, y+26))
    sheet.save(review / f'motion-{theme}.jpg', quality=92)
print(review)
