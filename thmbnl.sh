size=250
name=$1
duration=3
skip=2
output=output.gif
palette=palette.png

ffmpeg -y -ss $skip -t $duration -i $name -vf fps=10,scale=$size:-1:flags=lanczos,palettegen $palette
ffmpeg -ss $skip -t $duration -i $name -i $palette -filter_complex "fps=10,scale=$size:-1:flags=lanczos[x];[x][1:v]paletteuse" $output
rm $palette
