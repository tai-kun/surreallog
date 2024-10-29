#!/usr/bin/env bash

i=0

while [ $i != 4 ]; do
    if [ $((i%3)) == 0 ]; then
        echo -n "tick: $i"
    else
        echo "tick: $i"
    fi

    sleep 1s
    i=$((i+1))
done

# exit 2

echo '::add-mask::ichiro'
echo 'ichiro'

echo -n '1'
echo '::add-mask::jiro'
echo 'jiro'

echo '　::add-mask::jiro'
echo 'jiro'

printf '\t::add-mask::saburo\n'
echo 'saburo'

echo ' ::add-mask::taro'
echo 'taro'

printf '\n::add-mask::hanako\n'
echo 'hanako'

echo -n '::add-mask::suzuki'
echo '1'
echo 'suzuki'

echo '::error file=foo.yml,line=10::Wrong'
echo '::debug::ichiro'
echo '::debug foo=bar::jiro'
echo '::group::title'
echo '::endgroup::'
echo '::endgroup::hey'
